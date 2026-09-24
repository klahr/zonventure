import { test } from "node:test";
import assert from "node:assert/strict";
import { bearing, metresBetween, nearestTurn, project, unproject } from "./geo.ts";

test("bearings point the right way", () => {
  const near = (a: number, b: number) => assert.ok(Math.abs(a - b) < 0.5, `${a} ≈ ${b}`);
  near(bearing(59, 18, 60, 18), 0);
  near(bearing(59, 18, 59, 19), 89.6);
  near(bearing(59, 18, 58, 18), 180);
  near(bearing(59, 18, 59, 17), 270.4);
});

test("distances are metres", () => {
  const m = metresBetween(59.3293, 18.0686, 59.3326, 18.0649);
  assert.ok(m > 410 && m < 435, `${m}`);
});

test("the needle turns the short way", () => {
  assert.equal(nearestTurn(350, 10), 370);
  assert.equal(nearestTurn(10, 350), -10);
  assert.equal(nearestTurn(720, 90), 810);
  assert.equal(Math.abs(nearestTurn(0, 180)), 180, "half a turn either way");
});

test("a map pixel turns back into the point it shows", () => {
  const p = project(59.33, 18.07, 15);
  const q = unproject(p.x, p.y, 15);
  assert.ok(Math.abs(q.lat - 59.33) < 1e-9 && Math.abs(q.lng - 18.07) < 1e-9, JSON.stringify(q));
});
