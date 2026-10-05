import { test } from "node:test";
import assert from "node:assert/strict";
import { lookContrast } from "./contrast.ts";

test("the default look reads well", () => {
  assert.deepEqual(lookContrast({}), []);
});

test("pale text on a pale page is flagged, with its ratio and the aim", () => {
  const [issue] = lookContrast({ background: "#ffffff", text: "#bbbbbb" });
  assert.equal(issue?.key, "text");
  assert.equal(issue?.min, 4.5);
  assert.ok(issue!.ratio < 4.5);
});

test("titles may be lighter than text: 3:1 is enough", () => {
  assert.deepEqual(lookContrast({ background: "#ffffff", text: "#000000", accent_secondary: "#888888" }), []);
});

test("a see-through card over a strong backdrop is flagged; a solid one is not", () => {
  const busy = { background: "#ffffff", text: "#333333", backdrop: { kind: "spots" as const, opacity: 1, scale: 80, points: [{ x: 50, y: 50, color: "#111111" }] } };
  assert.ok(lookContrast({ ...busy, style: { card_opacity: 0.3 } }).some((i) => i.key === "cards"));
  assert.ok(!lookContrast({ ...busy, style: { card_opacity: 1 } }).some((i) => i.key === "cards"));
});

test("an outlined button in a pale accent is flagged; a filled one is not", () => {
  assert.ok(lookContrast({ background: "#ffffff", text: "#000000", accent: "#ffcc00", style: { button_fill: "outline" } }).some((i) => i.key === "buttons"));
  assert.ok(!lookContrast({ background: "#ffffff", text: "#000000", accent: "#ffcc00" }).some((i) => i.key === "buttons"));
});
