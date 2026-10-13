import { test } from "node:test";
import assert from "node:assert/strict";
import { english, feedbackForRefusal, feedbackForResult } from "./feedback.ts";

const view = { score: 0, penalty_score: 0, penalty_seconds: 0, now: "2026-09-22T10:00:00Z" };

test("a right answer says so, with the points", () => {
  assert.deepEqual(feedbackForResult("answer", { accepted: true, points: 7.5, view }, english), {
    tone: "good",
    text: "Correct! +7.5 points",
    burst: { kind: "celebrate", head: "Correct!", sub: "+7.5 points" },
  });
});

test("a wrong answer shows the author's ladder text first", () => {
  assert.equal(feedbackForResult("answer", { accepted: false, response: "Colder.", view }, english)?.text, "Colder.");
  assert.equal(feedbackForResult("answer", { accepted: false, view }, english)?.tone, "bad");
});

test("a wrong answer that took a pity path does not say try again", () => {
  const fb = feedbackForResult("answer", { accepted: false, port: "gave-up", view }, english);
  assert.equal(fb?.tone, "info");
  assert.equal(fb?.burst, undefined, "moving on is not a rebuff");
});

test("a wrong answer shakes, carrying the ladder text", () => {
  assert.deepEqual(feedbackForResult("answer", { accepted: false, response: "Colder.", view }, english)?.burst, {
    kind: "rebuff",
    head: "Not quite",
    sub: "Colder.",
  });
});

test("an answer on a step that just moves on gets no feedback, right or wrong", () => {
  assert.equal(feedbackForResult("answer", { accepted: true, points: 5, port: "out", quiet: true, view }, english), null);
  assert.equal(feedbackForResult("answer", { accepted: false, port: "out", quiet: true, view }, english), null);
  assert.equal(feedbackForResult("answer", { accepted: false, port: "out", quiet: true, view: { ...view, results_held: true } }, english), null);
});

test("a gate that turns the team back to the hub says not yet", () => {
  assert.equal(feedbackForResult("choose", { accepted: false, port: "garden", not_yet: true, view }, english)?.tone, "info");
  assert.equal(feedbackForResult("choose", { accepted: false, port: "garden", view }, english), null);
});

test("moves without an answer are silent", () => {
  assert.equal(feedbackForResult("continue", { accepted: false, port: "out", view }, english), null);
});

test("refusals map by code", () => {
  assert.equal(feedbackForRefusal("stale", english).tone, "info");
  assert.match(feedbackForRefusal("maintenance", english).text, /maintenance/);
  assert.equal(feedbackForRefusal("nonsense", english).tone, "bad");
});

test("english interpolates and leaves unknown names alone", () => {
  assert.equal(english("{{a}} and {{b}}", { a: 1 }), "1 and {{b}}");
});
