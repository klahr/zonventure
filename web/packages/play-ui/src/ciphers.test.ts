import { test } from "node:test";
import assert from "node:assert/strict";
import { ALPHABET, MORSE, SEMAPHORE, armVector, caesar, caesarAlphabet, caesarShift, parseKey, pigpen } from "./ciphers.ts";

test("morse covers A–Z and 0–9, each code its own", () => {
  const keys = [...ALPHABET, ..."0123456789"];
  for (const k of keys) assert.match(MORSE[k] ?? "", /^[·−]+$/, k);
  assert.equal(new Set(keys.map((k) => MORSE[k])).size, keys.length);
  assert.equal(MORSE.S, "···");
  assert.equal(MORSE.O, "−−−");
});

test("pigpen draws the grids and the X's", () => {
  // E is the middle of the plain grid: walled all round, no dot.
  assert.deepEqual(pigpen("E"), { segments: [[0, 0, 1, 0], [0, 1, 1, 1], [0, 0, 0, 1], [1, 0, 1, 1]], dot: null });
  // A is the top-left corner: a wall below and to the right.
  assert.deepEqual(pigpen("a")?.segments, [[0, 1, 1, 1], [1, 0, 1, 1]]);
  // N is E again, with a dot.
  assert.deepEqual(pigpen("N"), { ...pigpen("E")!, dot: [0.5, 0.5] });
  // S is the X's top wedge, a V; W the same with a dot.
  assert.deepEqual(pigpen("S")?.segments, [[0, 0, 0.5, 1], [0.5, 1, 1, 0]]);
  assert.equal(pigpen("S")?.dot, null);
  assert.deepEqual(pigpen("W")?.segments, pigpen("S")?.segments);
  assert.ok(pigpen("W")?.dot);
  assert.deepEqual(pigpen("Z")?.segments, pigpen("V")?.segments);
  assert.equal(pigpen("?"), null);
  // Every letter is its own glyph.
  const seen = new Set([...ALPHABET].map((c) => JSON.stringify(pigpen(c))));
  assert.equal(seen.size, 26);
});

test("semaphore gives every letter its own pair of arms", () => {
  const pairs = [...ALPHABET].map((c) => SEMAPHORE[c]);
  for (const [i, p] of pairs.entries()) {
    const c = ALPHABET.charAt(i);
    assert.ok(p, c);
    assert.ok(p[0] !== p[1] && p.every((n) => n >= 0 && n < 8), c);
  }
  assert.equal(new Set(pairs.map((p) => [...p!].sort().join())).size, 26);
  assert.deepEqual(SEMAPHORE.A, [0, 1]);
  assert.deepEqual(SEMAPHORE.R, [2, 6]);
});

test("semaphore arms point where the chart says", () => {
  assert.deepEqual(armVector(0), [0, 1]);
  assert.deepEqual(armVector(2), [-1, 0]);
  assert.deepEqual(armVector(4), [0, -1]);
  assert.deepEqual(armVector(6), [1, 0]);
});

test("caesar shifts the alphabet and a text", () => {
  assert.equal(caesarAlphabet(3), "DEFGHIJKLMNOPQRSTUVWXYZABC");
  assert.equal(caesarAlphabet(25)[0], "Z");
  assert.equal(caesar("Hello, World!", 3), "Khoor, Zruog!");
  assert.equal(caesar(caesar("Zonventure", 13), 13), "Zonventure");
  assert.equal(caesar("abc", -1), "zab");
});

test("a caesar shift out of range falls back to 3", () => {
  assert.equal(caesarShift(undefined), 3);
  assert.equal(caesarShift(0), 3);
  assert.equal(caesarShift(26), 3);
  assert.equal(caesarShift(7), 7);
});

test("a custom key splits on the first = and keeps notes", () => {
  assert.deepEqual(parseKey("Runes\n ᚠ = F \n\nx = a=b\n="), [
    { note: "Runes" },
    { symbol: "ᚠ", meaning: "F" },
    { symbol: "x", meaning: "a=b" },
    { symbol: "", meaning: "" },
  ]);
  assert.deepEqual(parseKey(undefined), []);
});
