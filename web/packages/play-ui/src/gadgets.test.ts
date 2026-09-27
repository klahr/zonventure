import { test } from "node:test";
import assert from "node:assert/strict";
import {
  headingOff,
  letterTiles,
  majority,
  morseFlashes,
  morseUnit,
  moveTile,
  nearSpot,
  shakeForce,
  shakeTarget,
  shuffleTiles,
  struckCards,
  toggleCard,
  vibratePattern,
  wheelShift,
} from "./gadgets.ts";

test("morse is timed by the PARIS standard, and never flashes faster than 3 Hz", () => {
  assert.equal(morseUnit(12), 1000 / 6, "12 wpm would be 100 ms: clamped");
  assert.equal(morseUnit(undefined), 1000 / 6, "the default 8 wpm would be 150 ms: clamped");
  assert.equal(morseUnit(4), 300);
  const u = morseUnit(4);
  // E T: dot, letter gap, dash.
  assert.deepEqual(morseFlashes("ET", 4), [
    { on: true, ms: u },
    { on: false, ms: 3 * u },
    { on: true, ms: 3 * u },
  ]);
  // S: three dots a unit apart.
  assert.deepEqual(
    morseFlashes("s", 4).map((f) => f.ms / u),
    [1, 1, 1, 1, 1],
  );
});

test("words are seven units apart, and what morse cannot send is skipped", () => {
  const u = morseUnit(4);
  const f = morseFlashes("E  E ~", 4);
  assert.deepEqual(f, [
    { on: true, ms: u },
    { on: false, ms: 7 * u },
    { on: true, ms: u },
  ]);
  assert.equal(morseFlashes("SOS?", 4).filter((x) => x.on).length, 9 + 6);
});

test("the vibration pattern starts with a buzz", () => {
  assert.deepEqual(vibratePattern([{ on: false, ms: 50 }, { on: true, ms: 100 }, { on: false, ms: 100 }, { on: true, ms: 300 }]), [100, 100, 300]);
});

test("a shake is measured beyond gravity, and the threshold scales the force asked", () => {
  assert.ok(shakeForce(0, 0, 9.81, true) < 0.01);
  assert.equal(shakeForce(3, 4, 0, false), 5);
  assert.ok(shakeTarget(1) < shakeTarget(60) && shakeTarget(60) < shakeTarget(100));
  assert.equal(shakeTarget(undefined), shakeTarget(60));
  assert.equal(shakeTarget(500), shakeTarget(100));
});

test("a heading is off by the short way round", () => {
  assert.equal(headingOff(350, 10), 20);
  assert.equal(headingOff(10, 350), 20);
  assert.equal(headingOff(90, 270), 180);
  assert.equal(headingOff(-5, 355), 0);
});

test("a vote has a majority only when one option leads alone", () => {
  assert.equal(majority([1, 3, 0]), 1);
  assert.equal(majority([2, 2]), -1);
  assert.equal(majority([0, 0]), -1);
  assert.equal(majority(undefined), -1);
  assert.equal(majority([2, 3, 3]), -1);
});

test("a tap finds the nearest difference within reach", () => {
  const spots = [{ x: 0.2, y: 0.2 }, { x: 0.25, y: 0.2 }, { x: 0.8, y: 0.8 }];
  assert.equal(nearSpot({ x: 0.24, y: 0.21 }, spots), 1);
  assert.equal(nearSpot({ x: 0.5, y: 0.5 }, spots), -1);
});

test("striking a card keeps the list sorted, and striking it again frees it", () => {
  assert.deepEqual(struckCards("2,0, x"), [2, 0]);
  assert.equal(toggleCard("2", 0), "0,2");
  assert.equal(toggleCard("0,2", 2), "0");
  assert.equal(toggleCard(undefined, 1), "1");
});

test("letter tiles drop spaces, move, and shuffle out of order", () => {
  assert.deepEqual(letterTiles("li sten"), ["L", "I", "S", "T", "E", "N"]);
  assert.deepEqual(moveTile(["A", "B", "C"], 0, 2), ["B", "C", "A"]);
  assert.deepEqual(moveTile(["A", "B"], 0, 5), ["A", "B"]);
  let seed = 1;
  const rand = () => ((seed = (seed * 16807) % 2147483647) - 1) / 2147483646;
  const tiles = ["A", "B", "C", "D"];
  const out = shuffleTiles(tiles, rand);
  assert.notDeepEqual(out, tiles);
  assert.deepEqual([...out].sort(), tiles);
});

test("the Caesar wheel reads the shift it was turned to", () => {
  assert.equal(wheelShift(0), 0);
  assert.equal(wheelShift(360 / 26 * 3 + 2), 3);
  assert.equal(wheelShift(-360 / 26), 25);
  assert.equal(wheelShift(360), 0);
});
