import { test } from "node:test";
import assert from "node:assert/strict";
import { guessFor, isSolved, restore, shuffled, swap } from "./tiles.ts";

test("a shuffle is a permutation that still needs solving", () => {
  for (let n = 4; n <= 64; n += 12) {
    const o = shuffled(n);
    assert.equal(new Set(o).size, n);
    assert.ok(o.every((v) => v >= 0 && v < n));
    assert.equal(isSolved(o), false);
  }
});

test("swapping back to order solves it, and the guess is the server's form", () => {
  let o = [1, 0, 2, 3];
  assert.equal(isSolved(o), false);
  o = swap(o, 0, 1);
  assert.equal(isSolved(o), true);
  assert.equal(guessFor(o), "0,1,2,3");
});

test("restore takes only an unsolved permutation of the right size", () => {
  assert.deepEqual(restore([1, 0, 2, 3], 4), [1, 0, 2, 3]);
  assert.equal(restore([0, 1, 2, 3], 4), null, "solved");
  assert.equal(restore([1, 0, 2], 4), null, "wrong size");
  assert.equal(restore([1, 1, 2, 3], 4), null, "repeat");
  assert.equal(restore("1,0,2,3", 4), null, "not a list");
});
