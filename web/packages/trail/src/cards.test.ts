import { test } from "node:test";
import assert from "node:assert/strict";
import { asKind, blank, move, numbers, problems, type Card } from "./cards.ts";

const quiz: Card = { id: "a", kind: "quiz", text: "How old?", options: ["7", "8", "9"], correct: "X" };
const answer: Card = { id: "b", kind: "answer", text: "Cat?", answers: ["Måns"] };
const info: Card = { id: "c", kind: "info", text: "Hi" };

test("a finished trail has no problems", () => {
  assert.deepEqual(problems([info, quiz, answer]), []);
});

test("problems name what a card needs, as the server does", () => {
  assert.deepEqual(problems([]), [{ code: "empty" }]);
  assert.deepEqual(problems([info]), [{ code: "no_question" }]);
  assert.deepEqual(problems([{ ...quiz, options: ["7", "", "9"] }]), [{ card: "a", code: "options" }]);
  assert.deepEqual(problems([{ ...quiz, correct: "" }]), [{ card: "a", code: "correct" }]);
  assert.deepEqual(problems([{ ...answer, text: " ", answers: [" "] }]), [
    { card: "b", code: "text" },
    { card: "b", code: "answers" },
  ]);
});

test("info cards are unnumbered", () => {
  assert.deepEqual(numbers([info, quiz, info, answer]), [0, 1, 0, 2]);
});

test("changing kind keeps the text and what fits", () => {
  const a = asKind(quiz, "answer");
  assert.equal(a.id, "a");
  assert.equal(a.text, "How old?");
  assert.deepEqual(a.answers, [""]);
  assert.deepEqual(asKind(a, "quiz").options, ["", "", ""]);
  assert.equal(blank("quiz").options?.length, 3);
});

test("move", () => {
  assert.deepEqual(move([1, 2, 3], 0, 2), [2, 3, 1]);
  assert.deepEqual(move([1, 2, 3], 2, 0), [3, 1, 2]);
  assert.deepEqual(move([1, 2, 3], 1, 5), [1, 2, 3]);
});
