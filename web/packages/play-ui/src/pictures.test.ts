import { test } from "node:test";
import assert from "node:assert/strict";
import { bubbleSide, fractionAt, nudge, round4 } from "./pictures.ts";

const box = { left: 100, top: 50, width: 300, height: 200 };

test("a click becomes fractions of the picture", () => {
  assert.deepEqual(fractionAt(250, 150, box), { x: 0.5, y: 0.5 });
  assert.deepEqual(fractionAt(100, 50, box), { x: 0, y: 0 });
  assert.deepEqual(fractionAt(200, 100, box), { x: 0.3333, y: 0.25 });
});

test("a click outside the picture is clamped to its edge", () => {
  assert.deepEqual(fractionAt(0, 400, box), { x: 0, y: 1 });
  assert.deepEqual(fractionAt(1000, -10, box), { x: 1, y: 0 });
});

test("an empty box gives the middle rather than NaN", () => {
  assert.deepEqual(fractionAt(10, 10, { left: 0, top: 0, width: 0, height: 0 }), { x: 0.5, y: 0.5 });
});

test("rounding keeps four decimals", () => {
  assert.equal(round4(0.123456), 0.1235);
  assert.equal(round4(1 / 3), 0.3333);
});

test("arrow keys move the torch and stop at the edges", () => {
  assert.deepEqual(nudge({ x: 0.5, y: 0.5 }, "ArrowRight", 0.25), { x: 0.75, y: 0.5 });
  assert.deepEqual(nudge({ x: 0.5, y: 0.5 }, "ArrowUp", 0.25), { x: 0.5, y: 0.25 });
  assert.deepEqual(nudge({ x: 0, y: 1 }, "ArrowLeft"), { x: 0, y: 1 });
  assert.deepEqual(nudge({ x: 0, y: 1 }, "ArrowDown"), { x: 0, y: 1 });
  assert.equal(nudge({ x: 0.5, y: 0.5 }, "Enter"), null);
});

test("a caption opens away from the nearest edges", () => {
  assert.deepEqual(bubbleSide({ x: 0.1, y: 0.9 }), { v: "above", h: "start" });
  assert.deepEqual(bubbleSide({ x: 0.5, y: 0.2 }), { v: "below", h: "middle" });
  assert.deepEqual(bubbleSide({ x: 0.9, y: 0.5 }), { v: "below", h: "end" });
});
