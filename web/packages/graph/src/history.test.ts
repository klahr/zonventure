import { test } from "node:test";
import assert from "node:assert/strict";
import {
  COALESCE_WINDOW_MS,
  canRedo,
  canUndo,
  classifyDelta,
  emptyHistory,
  record,
  redo,
  undo,
  rebase,
  emptyGraph,
  appendStep,
  moveNode,
  updateNode,
} from "./index.ts";

const base = appendStep(emptyGraph(), "puzzle").graph;

test("classifies a drag, an inspector edit and a structural change", () => {
  assert.equal(classifyDelta(base, base).kind, "none");
  assert.equal(classifyDelta(base, moveNode(base, "puzzle-1", 500, 500)).kind, "positions");
  const edited = updateNode(base, "puzzle-1", (n) => ({ ...n, title: "Renamed" }));
  assert.deepEqual(classifyDelta(base, edited), { kind: "node", slug: "puzzle-1" });
  assert.equal(classifyDelta(base, appendStep(base, "story").graph).kind, "structure");
});

// A drag produces dozens of position updates; one Ctrl+Z must undo the
// whole drag, not one mouse-move.
test("a continuous drag coalesces into one undo step", () => {
  let h = record(emptyHistory(), base, 0);
  for (let i = 1; i <= 20; i++) {
    h = record(h, moveNode(base, "puzzle-1", i * 10, 0), i * 10);
  }
  assert.equal(h.past.length, 1);
  const back = undo(h)!;
  assert.equal(back.doc, base);
});

test("typing in one node coalesces; a pause starts a new step", () => {
  let h = record(emptyHistory(), base, 0);
  h = record(h, updateNode(base, "puzzle-1", (n) => ({ ...n, title: "a" })), 100);
  h = record(h, updateNode(base, "puzzle-1", (n) => ({ ...n, title: "ab" })), 200);
  assert.equal(h.past.length, 1, "keystrokes within the window merge");
  h = record(h, updateNode(base, "puzzle-1", (n) => ({ ...n, title: "abc" })), 200 + COALESCE_WINDOW_MS + 1);
  assert.equal(h.past.length, 2, "a pause starts a fresh step");
});

// Being wrong in the merging direction would silently fold a delete
// into a drag.
test("a structural change never coalesces", () => {
  let h = record(emptyHistory(), base, 0);
  h = record(h, appendStep(base, "story").graph, 10);
  const next = h.present!;
  h = record(h, appendStep(next, "story").graph, 20);
  assert.equal(h.past.length, 2);
});

test("undo and redo walk the stack; a new edit clears redo", () => {
  let h = record(emptyHistory(), base, 0);
  const two = appendStep(base, "story").graph;
  h = record(h, two, 1000);
  assert.ok(canUndo(h));
  const u = undo(h)!;
  assert.equal(u.doc, base);
  assert.ok(canRedo(u.state));
  const r = redo(u.state)!;
  assert.equal(r.doc, two);
  const branched = record(u.state, appendStep(base, "puzzle").graph, 5000);
  assert.equal(canRedo(branched), false);
});

test("recording an identical document returns the same state object", () => {
  const h = record(emptyHistory(), base, 0);
  assert.equal(record(h, structuredClone(base), 10), h);
});

test("rebase fences the stack after an outside change", () => {
  const h = rebase(base, 0);
  assert.equal(canUndo(h), false);
  assert.equal(undo(h), null);
  assert.equal(redo(h), null);
});
