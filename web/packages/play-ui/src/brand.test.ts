import { test } from "node:test";
import assert from "node:assert/strict";
import { brandVars, isHex, readableOn, shade } from "./brand.ts";

test("readable text on light and dark accents", () => {
  assert.equal(readableOn("#ffffff"), "#000000");
  assert.equal(readableOn("#000000"), "#ffffff");
  assert.equal(readableOn("#ad1357"), "#ffffff");
  assert.equal(readableOn("#ffd400"), "#000000");
});

test("shade moves toward black or white", () => {
  assert.equal(shade("#808080", -1), "#000000");
  assert.equal(shade("#808080", 1), "#ffffff");
  assert.equal(shade("#000000", 0.5), "#808080");
  assert.equal(shade("red", 0.5), "red");
});

test("no palette means no overrides", () => {
  assert.deepEqual(brandVars(null), {});
  assert.deepEqual(brandVars({}), {});
});

test("a palette maps onto the tokens the play surface reads", () => {
  const v = brandVars({ accent: "#ffd400", background: "#101820", text: "#f0f0f0", submit_bg: "#ffffff" });
  assert.equal(v["--accent"], "#ffd400");
  assert.equal(v["--pv-on-accent"], "#000000");
  assert.equal(v["--pv-accent-2nd"], "#ffd400", "secondary falls back to the accent");
  assert.equal(v["--bg"], "#101820");
  assert.equal(v["--ink"], "#f0f0f0");
  assert.equal(v["--pv-dock-fg"], "#000000");
});

test("anything that is not a hex triplet never reaches a style", () => {
  for (const bad of ["red", "#fff", "url(x)", "#12345g", "#123456; background:url(x)"]) {
    assert.equal(isHex(bad), false, bad);
    assert.deepEqual(brandVars({ accent: bad, background: bad, text: bad, submit_bg: bad }), {}, bad);
  }
});
