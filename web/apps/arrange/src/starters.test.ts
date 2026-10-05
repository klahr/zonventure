import { test } from "node:test";
import assert from "node:assert/strict";
import { STARTER_THEMES } from "./designer/starterThemes.ts";
import { lookContrast } from "../../../packages/play-ui/src/contrast.ts";
import { FONTS } from "../../../packages/play-ui/src/brand.ts";

test("every starter theme reads well outdoors and uses fonts the player has", () => {
  const ids = new Set<string>();
  for (const s of STARTER_THEMES) {
    assert.ok(!ids.has(s.id), `${s.id} twice`);
    ids.add(s.id);
    assert.deepEqual(lookContrast(s.look), [], `${s.label} is hard to read`);
    for (const f of [s.look.font, s.look.heading_font]) if (f) assert.ok(f in FONTS, `${s.label}: no font ${f}`);
  }
});

test("cleaning a backdrop keeps a picture's image and blur", async () => {
  const { tidy } = await import("./designer/backdrops.ts");
  assert.deepEqual(tidy({ kind: "picture", media: 7, blur: 6, opacity: 0.8, scale: 100, fixed: true }), { kind: "picture", media: 7, blur: 6, opacity: 0.8, scale: 100, fixed: true });
});
