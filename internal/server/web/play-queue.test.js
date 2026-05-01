// Unit tests for play-queue.js. Run with `node --test play-queue.test.js`
// — Node's built-in test runner (Node 18+) is enough; no Jest, no
// Vitest, no package.json. The browser-only globals (`localStorage`,
// `crypto`) get tiny stubs installed before each test so the
// production code path runs unchanged.

const test = require('node:test');
const assert = require('node:assert/strict');

// Install a localStorage stub on globalThis BEFORE requiring the
// module under test. play-queue's functions reference `localStorage`
// at call time (not at load time), so installing it before the
// require still works — but doing it up-front mirrors the order a
// real browser would have.
function installLocalStorageStub() {
  const store = new Map();
  globalThis.localStorage = {
    getItem(k) { return store.has(k) ? store.get(k) : null; },
    setItem(k, v) { store.set(k, String(v)); },
    removeItem(k) { store.delete(k); },
    clear() { store.clear(); },
    // Test-only inspection hook; not a real Web Storage member.
    _store: store,
  };
}

// Run install before the require so the module's first behaviour
// (which doesn't actually touch localStorage at load time, but be
// defensive) sees a present stub.
installLocalStorageStub();
const queue = require('./play-queue.js');

// Reset state at the start of each test so order doesn't matter.
function reset() {
  queue.resetForTests();
  globalThis.localStorage._store.clear();
}

// ---- clientActionId ----

test('clientActionId returns a unique string each call', () => {
  reset();
  const a = queue.clientActionId();
  const b = queue.clientActionId();
  const c = queue.clientActionId();
  assert.equal(typeof a, 'string');
  assert.notEqual(a, '');
  // 100 calls; collisions here would mean the fallback's randomness
  // is broken or crypto.randomUUID isn't being called.
  const seen = new Set();
  for (let i = 0; i < 100; i++) seen.add(queue.clientActionId());
  assert.equal(seen.size, 100, 'clientActionId should be unique across calls');
  assert.notEqual(a, b);
  assert.notEqual(b, c);
});

test('clientActionId fallback path runs when crypto.randomUUID is absent', () => {
  reset();
  const saved = globalThis.crypto;
  delete globalThis.crypto;
  try {
    const id = queue.clientActionId();
    assert.match(id, /^a-/, 'fallback id should be prefixed "a-"');
  } finally {
    if (saved !== undefined) globalThis.crypto = saved;
  }
});

test('clientActionId crypto path runs when crypto.randomUUID is present', () => {
  reset();
  const saved = globalThis.crypto;
  globalThis.crypto = { randomUUID: () => 'fixed-uuid' };
  try {
    assert.equal(queue.clientActionId(), 'fixed-uuid');
  } finally {
    if (saved === undefined) delete globalThis.crypto;
    else globalThis.crypto = saved;
  }
});

// ---- persistPendingActions / loadPendingActions ----

test('persistPendingActions writes queue to localStorage', () => {
  reset();
  queue.pendingAnswers.push({ kind: 'answer', data: { answer: 'hi', client_action_id: 'x' } });
  queue.pendingAnswers.push({ kind: 'advance', data: { client_action_id: 'y' } });
  queue.persistPendingActions();
  const raw = globalThis.localStorage.getItem(queue.PENDING_QUEUE_KEY);
  assert.notEqual(raw, null);
  const parsed = JSON.parse(raw);
  assert.equal(parsed.length, 2);
  assert.equal(parsed[0].kind, 'answer');
  assert.equal(parsed[1].kind, 'advance');
});

test('persistPendingActions removes the key when the queue is empty', () => {
  reset();
  // Pre-populate so there's something to clear.
  globalThis.localStorage.setItem(queue.PENDING_QUEUE_KEY, '[]');
  queue.persistPendingActions();
  assert.equal(globalThis.localStorage.getItem(queue.PENDING_QUEUE_KEY), null,
    'empty queue should remove the key, not write "[]"');
});

test('loadPendingActions re-hydrates the queue from localStorage', () => {
  reset();
  globalThis.localStorage.setItem(queue.PENDING_QUEUE_KEY, JSON.stringify([
    { kind: 'answer', data: { answer: 'hello', client_action_id: 'a1' } },
    { kind: 'advance', data: { client_action_id: 'a2' } },
  ]));
  const loaded = queue.loadPendingActions();
  assert.equal(loaded, true, 'returned flag should signal we restored entries');
  assert.equal(queue.pendingAnswers.length, 2);
  assert.equal(queue.pendingAnswers[0].kind, 'answer');
  assert.equal(queue.pendingAnswers[1].data.client_action_id, 'a2');
});

test('loadPendingActions returns false when nothing is stored', () => {
  reset();
  assert.equal(queue.loadPendingActions(), false);
  assert.equal(queue.pendingAnswers.length, 0);
});

test('loadPendingActions tolerates malformed JSON', () => {
  reset();
  globalThis.localStorage.setItem(queue.PENDING_QUEUE_KEY, 'not-json');
  // Must not throw — the catch swallows and returns false.
  assert.doesNotThrow(() => queue.loadPendingActions());
  assert.equal(queue.pendingAnswers.length, 0);
});

test('loadPendingActions discards entries with unknown kinds', () => {
  reset();
  globalThis.localStorage.setItem(queue.PENDING_QUEUE_KEY, JSON.stringify([
    { kind: 'answer', data: { answer: 'ok' } },
    { kind: 'mystery', data: { foo: 'bar' } },
    { kind: 'advance', data: {} },
  ]));
  queue.loadPendingActions();
  // Two of three entries are valid; the mystery shape is dropped.
  assert.equal(queue.pendingAnswers.length, 2);
  assert.equal(queue.pendingAnswers[0].kind, 'answer');
  assert.equal(queue.pendingAnswers[1].kind, 'advance');
});

test('loadPendingActions caps the queue at PENDING_ANSWERS_MAX', () => {
  reset();
  // Stuff the storage with 10 entries — twice the cap.
  const overflow = [];
  for (let i = 0; i < 10; i++) {
    overflow.push({ kind: 'answer', data: { answer: 'q' + i, client_action_id: 'id-' + i } });
  }
  globalThis.localStorage.setItem(queue.PENDING_QUEUE_KEY, JSON.stringify(overflow));
  queue.loadPendingActions();
  assert.equal(queue.pendingAnswers.length, queue.PENDING_ANSWERS_MAX,
    'a hand-edited localStorage value must not be able to bypass the cap');
});

test('persistPendingActions tolerates a localStorage that throws', () => {
  reset();
  const saved = globalThis.localStorage;
  globalThis.localStorage = {
    getItem() { throw new Error('quota exceeded'); },
    setItem() { throw new Error('quota exceeded'); },
    removeItem() { throw new Error('quota exceeded'); },
  };
  try {
    queue.pendingAnswers.push({ kind: 'answer', data: { answer: 'x' } });
    // Must not throw.
    assert.doesNotThrow(() => queue.persistPendingActions());
    // In-memory queue is intact.
    assert.equal(queue.pendingAnswers.length, 1);
  } finally {
    globalThis.localStorage = saved;
  }
});

test('round-trip: persist then reset then load restores the same shape', () => {
  reset();
  const original = [
    { kind: 'answer', data: { answer: 'hello', client_action_id: 'aa' } },
    { kind: 'advance', data: { client_action_id: 'bb' } },
  ];
  for (const m of original) queue.pendingAnswers.push(m);
  queue.persistPendingActions();
  // Simulate a tab reload: drop in-memory state.
  queue.resetForTests();
  assert.equal(queue.pendingAnswers.length, 0);
  queue.loadPendingActions();
  assert.equal(queue.pendingAnswers.length, original.length);
  for (let i = 0; i < original.length; i++) {
    assert.deepEqual(queue.pendingAnswers[i], original[i]);
  }
});
