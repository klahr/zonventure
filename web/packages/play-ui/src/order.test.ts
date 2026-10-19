import { test } from "node:test";
import assert from "node:assert/strict";
import { dealt, dropPlace, guessFor, makesWay, moveCard, restore } from "./order.ts";

test("a card moves to its new place and the rest close up", () => {
  assert.deepEqual(moveCard([0, 1, 2, 3], 0, 2), [1, 2, 0, 3]);
  assert.deepEqual(moveCard([0, 1, 2, 3], 3, 0), [3, 0, 1, 2]);
  assert.deepEqual(moveCard([0, 1, 2], 1, 1), [0, 1, 2]);
  assert.equal(guessFor(moveCard(dealt(3), 2, 0)), "2,0,1");
});

test("a dragged card lands among the cards whose middles it has passed", () => {
  const mids = [25, 75, 125, 175];
  assert.equal(dropPlace(mids, 0, 25), 0, "where it started");
  assert.equal(dropPlace(mids, 0, 80), 1, "past the second");
  assert.equal(dropPlace(mids, 0, 400), 3, "below them all");
  assert.equal(dropPlace(mids, 3, -50), 0, "above them all");
  assert.equal(dropPlace(mids, 3, 120), 2, "short of the third");
});

test("the cards between make way, and only those", () => {
  // Dragging the top card down to the third place.
  assert.deepEqual([0, 1, 2, 3].map((i) => makesWay(i, 0, 2)), [0, -1, -1, 0]);
  // Dragging the last card up to the second.
  assert.deepEqual([0, 1, 2, 3].map((i) => makesWay(i, 3, 1)), [0, 1, 1, 0]);
  assert.deepEqual([0, 1, 2].map((i) => makesWay(i, 1, 1)), [0, 0, 0]);
});

test("a saved stack comes back only if it is still an order of the cards", () => {
  assert.deepEqual(restore([2, 0, 1], 3), [2, 0, 1]);
  assert.equal(restore([2, 0, 1], 4), null, "a card added since");
  assert.equal(restore([0, 0, 1], 3), null);
  assert.equal(restore([0, 1, 3], 3), null);
  assert.equal(restore("0,1,2", 3), null);
  assert.equal(restore(null, 3), null);
});
