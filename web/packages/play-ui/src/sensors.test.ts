import { test } from "node:test";
import assert from "node:assert/strict";
import { HOLD_START, beatMs, beepHz, bubble, elevation, holdStep, loudness, rms, tilt, warmth, warmthAt } from "./sensors.ts";

const near = (a: number, b: number, eps = 0.01) => assert.ok(Math.abs(a - b) < eps, `${a} ≈ ${b}`);

test("the radar warms as the zone gets closer", () => {
  const bands = [3000, 1500, 600, 250, 100, 40, 25].map((d) => warmth(d, 25).band);
  assert.deepEqual(bands, ["freezing", "freezing", "cold", "warmer", "hot", "burning", "there"]);
  const heats = [2000, 1000, 500, 100, 50].map((d) => warmth(d, 25).heat);
  for (let i = 1; i < heats.length; i++) assert.ok(heats[i]! > heats[i - 1]!, `${heats}`);
  near(warmth(5000, 25).heat, 0);
});

test("a tiny zone counts as reached within GPS reach", () => {
  assert.equal(warmth(15, 5).band, "there");
  assert.notEqual(warmth(30, 5).band, "there");
});

test("the radar beeps faster and higher as it warms", () => {
  assert.equal(beatMs(0), 2400);
  assert.equal(beatMs(1), 500);
  assert.ok(beatMs(0.6) < beatMs(0.4));
  assert.equal(beepHz(0), 440);
  assert.equal(beepHz(1), 880);
  assert.equal(beepHz(7), 880);
});

test("a huge zone stretches the scale instead of being hot from anywhere", () => {
  assert.equal(warmth(600, 500).band, "burning");
  assert.equal(warmth(9000, 500).band, "freezing");
});

test("a polygon zone is judged from its middle", () => {
  // About 110 m square round 59.0005, 18.001, while lat/lng point elsewhere.
  const zone = {
    lat: 0,
    lng: 0,
    radius_m: 0,
    polygon: [
      [59.0, 18.0],
      [59.0, 18.002],
      [59.001, 18.002],
      [59.001, 18.0],
    ] as [number, number][],
  };
  assert.equal(warmthAt({ lat: 59.0005, lng: 18.001 }, zone).band, "there");
  assert.equal(warmthAt({ lat: 59.005, lng: 18.001 }, zone).band, "warmer");
});

test("loudness maps dBFS −60…0 onto 0…100", () => {
  near(loudness(1), 100);
  near(loudness(0.1), 66.67);
  near(loudness(0.001), 0);
  assert.equal(loudness(0), 0);
  assert.equal(loudness(0.00001), 0);
  near(rms([0.5, -0.5, 0.5, -0.5]), 0.5);
  assert.equal(rms([]), 0);
});

test("tilt and elevation read the phone's pose", () => {
  near(tilt(0, 0), 0);
  near(tilt(10, 0), 10);
  near(tilt(0, -10), 10);
  near(elevation(0, 0), -90);
  near(elevation(90, 0), 0);
  near(elevation(120, 0), 30);
  near(elevation(-150, 0), 60);
  // Leant sideways the camera still looks level when upright.
  near(elevation(90, 30), 0);
});

test("the bubble floats to the high side and stays in the vial", () => {
  const b = bubble(10, 0);
  near(b.x, 0);
  near(b.y, -0.5);
  assert.ok(bubble(0, 10).x < 0);
  const rim = bubble(40, 40);
  near(Math.hypot(rim.x, rim.y), 1);
});

test("a hold counts only when unbroken", () => {
  let h = holdStep(HOLD_START, true, 0, 1500);
  h = holdStep(h, true, 1000, 1500);
  assert.equal(h.done, false);
  h = holdStep(h, false, 1200, 1500);
  h = holdStep(h, true, 1300, 1500);
  h = holdStep(h, true, 2700, 1500);
  assert.equal(h.done, false);
  h = holdStep(h, true, 2800, 1500);
  assert.equal(h.done, true);
  assert.equal(holdStep(h, false, 2900, 1500).done, true);
});
