import { test, describe } from "node:test";
import assert from "node:assert/strict";
import {
  Outbox,
  PENDING_QUEUE_KEY,
  PENDING_ANSWERS_MAX,
  clientActionId,
  type OutboxStorage,
  type PendingAction,
} from "./index.ts";

/** In-memory Storage double. Also lets a test force a throw. */
class FakeStorage implements OutboxStorage {
  map = new Map<string, string>();
  throwOnWrite = false;

  getItem(k: string): string | null {
    return this.map.get(k) ?? null;
  }
  setItem(k: string, v: string): void {
    if (this.throwOnWrite) throw new Error("QuotaExceededError");
    this.map.set(k, v);
  }
  removeItem(k: string): void {
    this.map.delete(k);
  }
}

const answer = (id: string): PendingAction => ({
  kind: "answer",
  data: { client_action_id: id, answer: "42" },
});

describe("clientActionId", () => {
  test("is unique across calls", () => {
    const ids = new Set(Array.from({ length: 100 }, () => clientActionId()));
    assert.equal(ids.size, 100);
  });

  test("falls back when crypto.randomUUID is unavailable", () => {
    const original = globalThis.crypto;
    try {
      Object.defineProperty(globalThis, "crypto", {
        value: undefined,
        configurable: true,
      });
      assert.match(clientActionId(), /^a-[0-9a-z]+-[0-9a-z]+$/);
    } finally {
      Object.defineProperty(globalThis, "crypto", {
        value: original,
        configurable: true,
      });
    }
  });
});

describe("enqueue and persist", () => {
  test("round-trips through storage", () => {
    const s = new FakeStorage();
    const out = new Outbox(s);
    out.enqueue(answer("a"));

    const restored = new Outbox(s);
    assert.equal(restored.load(), true);
    assert.equal(restored.size, 1);
    assert.equal(restored.pending[0]?.data.client_action_id, "a");
  });

  test("uses the versioned key", () => {
    const s = new FakeStorage();
    new Outbox(s).enqueue(answer("a"));
    assert.ok(s.map.has(PENDING_QUEUE_KEY));
  });

  // The cap is what stops a player spamming during dead air from
  // pre-filling the rate limiter on reconnect. Oldest goes first, so
  // the most recent intent survives.
  test("drops the oldest past the cap", () => {
    const out = new Outbox(new FakeStorage());
    for (let i = 0; i < PENDING_ANSWERS_MAX + 3; i++) {
      out.enqueue(answer(`id-${i}`));
    }
    assert.equal(out.size, PENDING_ANSWERS_MAX);
    assert.equal(out.pending[0]?.data.client_action_id, "id-3");
    assert.equal(
      out.pending[PENDING_ANSWERS_MAX - 1]?.data.client_action_id,
      `id-${PENDING_ANSWERS_MAX + 2}`,
    );
  });

  // Removing the key rather than storing [] matters: a stale read must
  // not re-hydrate an action that was already flushed.
  test("removes the key when the queue empties", () => {
    const s = new FakeStorage();
    const out = new Outbox(s);
    out.enqueue(answer("a"));
    out.ack("a");
    out.persist();
    assert.equal(s.map.has(PENDING_QUEUE_KEY), false);
  });

  // Private mode and a full quota both throw. Losing actions on reload
  // is the pre-storage baseline; losing them from memory is not.
  test("survives a throwing storage", () => {
    const s = new FakeStorage();
    s.throwOnWrite = true;
    const out = new Outbox(s);
    assert.doesNotThrow(() => out.enqueue(answer("a")));
    assert.equal(out.size, 1);
  });

  test("tolerates no storage at all", () => {
    const out = new Outbox(null);
    assert.doesNotThrow(() => out.enqueue(answer("a")));
    assert.equal(out.size, 1);
    assert.equal(out.load(), false);
  });
});

describe("load", () => {
  test("returns false when nothing is stored", () => {
    assert.equal(new Outbox(new FakeStorage()).load(), false);
  });

  test("returns false on malformed JSON", () => {
    const s = new FakeStorage();
    s.map.set(PENDING_QUEUE_KEY, "{not json");
    const out = new Outbox(s);
    assert.equal(out.load(), false);
    assert.equal(out.size, 0);
  });

  test("returns false when the payload is not an array", () => {
    const s = new FakeStorage();
    s.map.set(PENDING_QUEUE_KEY, JSON.stringify({ kind: "answer" }));
    assert.equal(new Outbox(s).load(), false);
  });

  // Corruption, or a newer client writing a shape this version does
  // not understand. Drop the bad entries, keep the good ones.
  test("skips entries that are not a known action shape", () => {
    const s = new FakeStorage();
    s.map.set(
      PENDING_QUEUE_KEY,
      JSON.stringify([
        null,
        { kind: "nope", data: {} },
        { kind: "answer" },
        answer("good"),
        "string",
      ]),
    );
    const out = new Outbox(s);
    assert.equal(out.load(), true);
    assert.equal(out.size, 1);
    assert.equal(out.pending[0]?.data.client_action_id, "good");
  });

  test("accepts both action kinds", () => {
    const s = new FakeStorage();
    s.map.set(
      PENDING_QUEUE_KEY,
      JSON.stringify([
        { kind: "answer", data: { client_action_id: "a" } },
        { kind: "advance", data: { client_action_id: "b" } },
      ]),
    );
    const out = new Outbox(s);
    assert.equal(out.load(), true);
    assert.equal(out.size, 2);
  });

  // A hand-edited storage value must not be able to stuff the queue.
  test("re-applies the cap on load", () => {
    const s = new FakeStorage();
    const many = Array.from({ length: PENDING_ANSWERS_MAX + 10 }, (_, i) =>
      answer(`id-${i}`),
    );
    s.map.set(PENDING_QUEUE_KEY, JSON.stringify(many));
    const out = new Outbox(s);
    assert.equal(out.load(), true);
    assert.equal(out.size, PENDING_ANSWERS_MAX);
  });
});

describe("ack", () => {
  test("drops the matching action and reports it", () => {
    const out = new Outbox(new FakeStorage());
    out.enqueue(answer("a"));
    out.enqueue(answer("b"));
    assert.equal(out.ack("a"), true);
    assert.equal(out.size, 1);
    assert.equal(out.pending[0]?.data.client_action_id, "b");
  });

  // False means "nothing changed", which is how the caller avoids
  // pointless storage churn on an ack for a submit that never queued.
  test("reports false for an unknown id", () => {
    const out = new Outbox(new FakeStorage());
    out.enqueue(answer("a"));
    assert.equal(out.ack("nope"), false);
    assert.equal(out.size, 1);
  });

  test("ignores an empty id", () => {
    const out = new Outbox(new FakeStorage());
    out.enqueue(answer("a"));
    assert.equal(out.ack(""), false);
    assert.equal(out.ack(null), false);
    assert.equal(out.ack(undefined), false);
    assert.equal(out.size, 1);
  });

  // Reverse iteration exists precisely so removing one entry does not
  // skip the next; duplicates of the same id must all go.
  test("drops every entry sharing an id", () => {
    const out = new Outbox(new FakeStorage());
    out.enqueue(answer("dup"));
    out.enqueue(answer("dup"));
    out.enqueue(answer("other"));
    assert.equal(out.ack("dup"), true);
    assert.equal(out.size, 1);
    assert.equal(out.pending[0]?.data.client_action_id, "other");
  });
});

// The scenario the whole module exists for, start to finish.
test("offline submit survives a reload and acks exactly once", () => {
  const s = new FakeStorage();

  const before = new Outbox(s);
  const id = clientActionId();
  before.enqueue({ kind: "answer", data: { client_action_id: id, answer: "x" } });

  // Tab dies. New process, same storage.
  const after = new Outbox(s);
  assert.equal(after.load(), true);
  assert.equal(after.size, 1);

  // Reconnect re-sends; the server replays its cached response and we
  // ack. A second ack for the same id must be a no-op, because the
  // server may deliver the result more than once.
  assert.equal(after.ack(id), true);
  after.persist();
  assert.equal(after.ack(id), false);
  assert.equal(after.size, 0);
  assert.equal(s.map.has(PENDING_QUEUE_KEY), false);
});
