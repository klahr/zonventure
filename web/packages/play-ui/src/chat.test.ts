import { test } from "node:test";
import assert from "node:assert/strict";
import { typingTime } from "./chat.ts";

test("typing takes longer the more characters a message has", () => {
  assert.equal(typingTime(""), 400);
  assert.equal(typingTime("Hej!"), 540);
  assert.ok(typingTime("x".repeat(100)) > typingTime("x".repeat(50)));
  assert.equal(typingTime("🤠🤠"), 470, "an emoji is one character");
  assert.equal(typingTime("x".repeat(1000)), 7000, "a paragraph is capped");
});
