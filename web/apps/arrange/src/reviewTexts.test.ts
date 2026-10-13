import { test } from "node:test";
import assert from "node:assert/strict";
import { changedTexts, isPlayerText, textLabel } from "./pages/admin/reviewTexts.ts";

const t = { t: (key: string, vars?: Record<string, string | number>) => key.replace(/\{\{(\w+)\}\}/g, (_, k: string) => String(vars?.[k] ?? "")) };
const steps = ["Start", "Bridge", "The end"];

test("graph paths read as step and field", () => {
  assert.equal(textLabel("Adventure: nodes[1].story.blocks[1].text", steps, t), "Step 2 · Bridge · story text");
  assert.equal(textLabel("Adventure: nodes[1].puzzle.outputs[0].matchers[0].value", steps, t), "Step 2 · Bridge · puzzle answer");
  assert.equal(textLabel("Adventure: nodes[1].puzzle.hints[0].text", steps, t), "Step 2 · Bridge · puzzle hint");
  assert.equal(textLabel("Adventure: nodes[0].title", steps, t), "Step 1 · Start · title");
  assert.equal(textLabel("Adventure: nodes[7].title", steps, t), "Step 8 · title");
  assert.equal(textLabel("Adventure: zones[0].title", steps, t), "Zone 1 · title");
});

test("other places keep the server's words", () => {
  assert.equal(textLabel("Listing: name", steps, t), "Listing: name");
  assert.equal(textLabel("Adventure: meeting point", steps, t), "Adventure: meeting point");
});

test("settings stored as words are not player texts", () => {
  assert.equal(isPlayerText("Adventure: language"), false);
  assert.equal(isPlayerText("Adventure: nodes[2].end.outcome"), false);
  assert.equal(isPlayerText("Adventure: nodes[2].end.blocks[0].text"), true);
  assert.equal(isPlayerText("Listing: name"), true);
});

test("changed texts say what they were", () => {
  const before = [{ where: "a", text: "one" }, { where: "b", text: "two" }, { where: "c", text: "gone" }];
  const after = [{ where: "a", text: "one" }, { where: "b", text: "TWO" }, { where: "d", text: "new" }];
  assert.deepEqual(changedTexts(before, after), [
    { where: "b", text: "TWO", was: "two" },
    { where: "d", text: "new", was: "" },
    { where: "c", text: "", was: "gone" },
  ]);
});
