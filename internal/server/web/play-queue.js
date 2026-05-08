// Player-side offline action queue. Lives in its own file so it can
// be unit-tested in Node without spinning up a browser — see
// play-queue.test.js. In the browser it's loaded via a <script src>
// before the main play-page IIFE, so its top-level functions and
// pendingAnswers array become globals available to the inline code.
//
// Why this is its own module:
//   * The queue is the load-bearing piece for offline-tolerant play
//     and a regression here ships silently — a player loses an answer
//     they typed in a dead spot, the server sees nothing, and there's
//     no error path that fires.
//   * The localStorage round-trip + the cap enforcement are easy to
//     get wrong on refactor (off-by-one on slice, wrong key, etc.)
//     and are testable in isolation without DOM / WebSocket setup.
//
// Browser surface (used directly by play.html):
//   pendingAnswers              — the FIFO array of {kind, data}
//   PENDING_ANSWERS_MAX         — cap on queue length
//   clientActionId()            — UUID per logical user action
//   persistPendingActions()     — write current queue to localStorage
//   loadPendingActions()        — re-hydrate queue from localStorage
//
// Node surface (for tests): same names exported via module.exports.

// localStorage key for the persisted queue. v1 in the name so a
// future format bump can ignore older payloads cleanly.
var PENDING_QUEUE_KEY = 'zv-play-pending-v1';

// Cap on queued mutating actions. A player spamming during dead air
// shouldn't pre-fill the rate limiter on reconnect; FIFO drop after
// 5 keeps any reasonable player covered while cutting the abuse case.
var PENDING_ANSWERS_MAX = 5;

// The queue itself. Module-private state — call sites mutate the
// array directly (push/shift) and call persistPendingActions to
// flush. Tests reset via resetForTests below.
var pendingAnswers = [];

// clientActionId generates a stable idempotency key per user-issued
// action. The server's player_action_dedup table replays the cached
// response when the same id arrives twice (post-reconnect retry of
// a queued submit). crypto.randomUUID is shipped on every modern
// browser; the fallback covers older devices that still play.
function clientActionId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID();
  return 'a-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

// persistPendingActions writes the current queue to localStorage so
// a tab reload / app crash / swipe-up dismiss doesn't lose queued
// actions. Empty queue -> remove the key entirely so a stale read
// doesn't re-hydrate an action that was already flushed.
//
// Best-effort: localStorage throws in private mode and when the
// quota is full. Either failure leaves the in-memory queue intact;
// the worst that happens is "actions lost on reload" which is the
// pre-localStorage baseline.
function persistPendingActions() {
  try {
    if (pendingAnswers.length === 0) {
      localStorage.removeItem(PENDING_QUEUE_KEY);
    } else {
      localStorage.setItem(PENDING_QUEUE_KEY, JSON.stringify(pendingAnswers));
    }
  } catch (_) { /* private mode / storage full — best effort */ }
}

// loadPendingActions re-hydrates the queue from localStorage on app
// boot. Returns true if any actions were restored (so the caller
// can flag "you were offline" UI state). Discards malformed entries
// silently — anything that isn't a {kind: 'answer'|'advance', data}
// shape is probably corruption from a future-version client.
//
// Cap re-applied here too: a hand-edited localStorage value can't
// stuff the queue past PENDING_ANSWERS_MAX. slice() before the loop
// makes the cap idempotent regardless of how many entries were
// stored.
function loadPendingActions() {
  try {
    var raw = localStorage.getItem(PENDING_QUEUE_KEY);
    if (!raw) return false;
    var parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return false;
    var loaded = false;
    for (var i = 0; i < parsed.length && pendingAnswers.length < PENDING_ANSWERS_MAX; i++) {
      var m = parsed[i];
      if (m && (m.kind === 'answer' || m.kind === 'advance') && m.data) {
        pendingAnswers.push(m);
        loaded = true;
      }
    }
    return loaded;
  } catch (_) {
    return false;
  }
}

// ackPending removes any queued action whose data.client_action_id
// matches the supplied id. Returns true if something was dropped so
// the caller can persist exactly when state actually changed (avoiding
// pointless localStorage churn when the ack lands for a fresh-fired
// submit that never went into the queue).
//
// The dedup table on the server is keyed on (slot_id, client_action_id),
// so the queue can hold a submission until we see an answer_result or
// answer_error for that id. Reconnect re-sends every queued entry; the
// server replays the cached response for the ones that already landed.
function ackPending(actionID) {
  if (!actionID) return false;
  var dropped = false;
  for (var i = pendingAnswers.length - 1; i >= 0; i--) {
    var m = pendingAnswers[i];
    if (m && m.data && m.data.client_action_id === actionID) {
      pendingAnswers.splice(i, 1);
      dropped = true;
    }
  }
  return dropped;
}

// Test helper: drop all in-memory state. Browser code never calls
// this; only the Node test runner does, between cases.
function resetForTests() {
  pendingAnswers.length = 0;
}

// Conditional Node export. Browsers see no `module` global so this
// is a no-op there; Node's require() returns the named members.
if (typeof module !== 'undefined' && typeof module.exports !== 'undefined') {
  module.exports = {
    PENDING_QUEUE_KEY: PENDING_QUEUE_KEY,
    PENDING_ANSWERS_MAX: PENDING_ANSWERS_MAX,
    pendingAnswers: pendingAnswers,
    clientActionId: clientActionId,
    persistPendingActions: persistPendingActions,
    loadPendingActions: loadPendingActions,
    ackPending: ackPending,
    resetForTests: resetForTests,
  };
}
