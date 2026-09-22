// Player-side offline action queue.
//
// A behaviour-identical TypeScript port of
// internal/server/web/play-queue.js. Same localStorage key, same cap,
// same FIFO-drop, same ack semantics — deliberately, because this is
// the load-bearing piece for offline-tolerant play and a regression
// here ships silently: a player loses an answer they typed in a dead
// spot, the server sees nothing, and no error path fires.
//
// Two things changed in the port, both to make it testable rather than
// to change behaviour:
//
//   1. Storage is injected (defaulting to globalThis.localStorage), so
//      tests need no DOM shim and a private-mode failure is reachable
//      in a test rather than only in the wild.
//   2. The queue is an object rather than module-global state, so a
//      test does not have to reset shared globals between cases.
//
// The server's player_action_dedup table is keyed on
// (slot_id, client_action_id): a reconnect re-sends every queued entry
// and the server replays the cached response for ones that already
// landed. That is what makes at-least-once delivery safe here.

/** localStorage key. v1 so a future format bump can ignore old payloads. */
export const PENDING_QUEUE_KEY = "zv-play-pending-v1";

/**
 * Cap on queued mutating actions. A player spamming during dead air
 * shouldn't pre-fill the rate limiter on reconnect; FIFO drop after 5
 * covers any reasonable player while cutting the abuse case.
 */
export const PENDING_ANSWERS_MAX = 5;

export type PendingKind = "answer" | "advance";

export interface PendingAction {
  kind: PendingKind;
  data: { client_action_id?: string } & Record<string, unknown>;
}

/** The slice of the Storage API this needs. */
export interface OutboxStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/**
 * clientActionId generates a stable idempotency key per user-issued
 * action. crypto.randomUUID ships on every modern browser; the
 * fallback covers older devices that still play.
 */
export function clientActionId(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  return (
    "a-" +
    Date.now().toString(36) +
    "-" +
    Math.random().toString(36).slice(2, 10)
  );
}

function isPendingAction(v: unknown): v is PendingAction {
  if (typeof v !== "object" || v === null) return false;
  const m = v as { kind?: unknown; data?: unknown };
  if (m.kind !== "answer" && m.kind !== "advance") return false;
  return typeof m.data === "object" && m.data !== null;
}

export class Outbox {
  /**
   * The FIFO queue. Exposed because call sites push and inspect it
   * directly, exactly as play.html does today.
   */
  readonly pending: PendingAction[] = [];

  private readonly storage: OutboxStorage | null;

  constructor(storage: OutboxStorage | null = defaultStorage()) {
    this.storage = storage;
  }

  /** Queue length, for UI that shows "N waiting to send". */
  get size(): number {
    return this.pending.length;
  }

  /**
   * Append an action, enforcing the cap by dropping the oldest entry.
   * Returns the action so callers can read back the id they queued.
   */
  enqueue(action: PendingAction): PendingAction {
    this.pending.push(action);
    while (this.pending.length > PENDING_ANSWERS_MAX) this.pending.shift();
    this.persist();
    return action;
  }

  /**
   * Write the queue to storage so a reload, crash, or swipe-up dismiss
   * doesn't lose queued actions. An empty queue removes the key, so a
   * stale read can't re-hydrate something already flushed.
   *
   * Best effort: storage throws in private mode and when the quota is
   * full. Either way the in-memory queue survives and the worst case
   * is "actions lost on reload", which is the pre-storage baseline.
   */
  persist(): void {
    if (!this.storage) return;
    try {
      if (this.pending.length === 0) {
        this.storage.removeItem(PENDING_QUEUE_KEY);
      } else {
        this.storage.setItem(PENDING_QUEUE_KEY, JSON.stringify(this.pending));
      }
    } catch {
      /* private mode / storage full — best effort */
    }
  }

  /**
   * Re-hydrate from storage on boot. Returns true if anything was
   * restored, so the caller can flag "you were offline" UI state.
   *
   * Malformed entries are dropped silently — anything that isn't a
   * {kind, data} shape is corruption or a future-version client. The
   * cap is re-applied here too, so a hand-edited storage value cannot
   * stuff the queue past PENDING_ANSWERS_MAX.
   */
  load(): boolean {
    if (!this.storage) return false;
    try {
      const raw = this.storage.getItem(PENDING_QUEUE_KEY);
      if (!raw) return false;
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return false;
      let loaded = false;
      for (const entry of parsed) {
        if (this.pending.length >= PENDING_ANSWERS_MAX) break;
        if (isPendingAction(entry)) {
          this.pending.push(entry);
          loaded = true;
        }
      }
      return loaded;
    } catch {
      return false;
    }
  }

  /**
   * Drop any queued action whose data.client_action_id matches.
   * Returns true if something was removed, so the caller can persist
   * exactly when state changed rather than churning storage on an ack
   * for a fresh submit that never entered the queue.
   */
  ack(actionID: string | null | undefined): boolean {
    if (!actionID) return false;
    let dropped = false;
    for (let i = this.pending.length - 1; i >= 0; i--) {
      if (this.pending[i]?.data?.client_action_id === actionID) {
        this.pending.splice(i, 1);
        dropped = true;
      }
    }
    return dropped;
  }

  /** Drop all in-memory state. Tests only. */
  reset(): void {
    this.pending.length = 0;
  }
}

function defaultStorage(): OutboxStorage | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    // Accessing localStorage itself throws when cookies are blocked.
    return null;
  }
}
