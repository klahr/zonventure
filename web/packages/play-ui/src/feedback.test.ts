import { test } from "node:test";
import assert from "node:assert/strict";
import { english, feedbackForRefusal, feedbackForResult } from "./feedback.ts";

const view = { score: 0, penalty_score: 0, penalty_seconds: 0, now: "2026-09-22T10:00:00Z" };

test("a right answer says so, with the points", () => {
  assert.deepEqual(feedbackForResult("answer", { accepted: true, points: 7.5, view }, english), {
    tone: "good",
    text: "Correct! +7.5 points",
  });
});

test("a wrong answer shows the author's ladder text first", () => {
  assert.equal(feedbackForResult("answer", { accepted: false, response: "Colder.", view }, english)?.text, "Colder.");
  assert.equal(feedbackForResult("answer", { accepted: false, view }, english)?.tone, "bad");
});

test("a wrong answer that took a pity path does not say try again", () => {
  const fb = feedbackForResult("answer", { accepted: false, port: "gave-up", view }, english);
  assert.equal(fb?.tone, "info");
});

test("moves without an answer are silent", () => {
  assert.equal(feedbackForResult("continue", { accepted: false, port: "out", view }, english), null);
});

test("refusals map by code", () => {
  assert.equal(feedbackForRefusal("stale", english).tone, "info");
  assert.equal(feedbackForRefusal("nonsense", english).tone, "bad");
});

test("english interpolates and leaves unknown names alone", () => {
  assert.equal(english("{{a}} and {{b}}", { a: 1 }), "1 and {{b}}");
});
