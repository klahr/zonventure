import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { ATTENTIONS, ENTRANCES, TRANSITIONS, animationCSS } from "./animations.ts";
import * as graph from "../../graph/src/types.ts";

test("nothing to do, nothing set", () => {
  assert.equal(animationCSS(undefined), undefined);
  assert.equal(animationCSS({}), undefined);
});

test("an entrance, then an attention effect once it is in", () => {
  assert.equal(
    animationCSS({ entrance: "bounceIn", attention: "pulse", delay: 1, repeat: "forever" }),
    "pv-a-bounceIn 1s ease-out 1s both, pv-a-pulse 1s ease-in-out 2s infinite forwards",
  );
});

test("an attention effect alone keeps the step's gentle rise first", () => {
  assert.equal(animationCSS({ attention: "tada", duration: 0.5, repeat: "three" }), "pv-enter 0.7s cubic-bezier(0.2, 0.8, 0.2, 1) 0s both, pv-a-tada 0.5s ease-in-out 0.7s 3 forwards");
  assert.match(animationCSS({ entrance: "fadeIn", duration: 99 })!, /^pv-a-fadeIn 5s /, "the duration is bounded");
});

test("a name the player does not know never reaches a style, and the delay is bounded", () => {
  assert.equal(animationCSS({ entrance: "x; background:red" as never }), undefined);
  assert.match(animationCSS({ entrance: "fadeIn", delay: 99 })!, / 10s both$/);
});

test("every effect has its keyframes, and the lists match the graph's", () => {
  const css = readFileSync(new URL("./animations.css", import.meta.url), "utf8");
  for (const n of [...ENTRANCES, ...ATTENTIONS]) assert.ok(css.includes(`@keyframes pv-a-${n} `), n);
  for (const n of TRANSITIONS.filter((x) => x !== "none")) assert.ok(css.includes(`[data-transition="${n}"]`), n);
  assert.deepEqual([...ENTRANCES], [...graph.ENTRANCES]);
  assert.deepEqual([...ATTENTIONS], [...graph.ATTENTIONS]);
});

test("a flip turns the page in 3D, and fades instead under reduced motion", () => {
  const css = readFileSync(new URL("./animations.css", import.meta.url), "utf8");
  assert.match(css, /@keyframes pv-t-flip \{[^\n]*perspective\([^)]*\) rotateY\(-90deg\)/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\) \{\s*\.pv \.pv-step\.is-entered\[data-transition="flip"\] \{ animation: pv-t-fade [^}]*!important; \}/);
});
