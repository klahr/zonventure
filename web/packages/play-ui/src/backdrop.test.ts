import { test } from "node:test";
import assert from "node:assert/strict";
import type { play } from "@zonventure/api";
import { BACKDROP_KINDS, backdropVars } from "./backdrop.ts";
import { blend, brandVars, contrastRatio, withTheme } from "./brand.ts";

const layerCount = (v: Record<string, string>) => (v["--pv-backdrop-size"] ?? "").split(", ").length;

test("no backdrop, or an unknown kind, paints nothing", () => {
  assert.deepEqual(backdropVars(undefined, {}), {});
  assert.deepEqual(backdropVars({ kind: "plaid" as play.BackdropKind, opacity: 0.1, scale: 20 }, {}), {});
});

test("every kind paints at least one layer, with one size, position, repeat and attachment each", () => {
  for (const kind of Object.keys(BACKDROP_KINDS) as play.BackdropKind[]) {
    const v = backdropVars({ kind, opacity: 0.2, scale: 24, ...(kind === "spots" ? { points: [{ x: 50, y: 50 }] } : {}), ...(kind === "picture" ? { media: 1 } : {}) }, {});
    assert.ok(v["--pv-backdrop"], kind);
    const n = layerCount(v);
    assert.equal(v["--pv-backdrop-pos"]!.split(", ").length, n, kind);
    assert.equal(v["--pv-backdrop-repeat"]!.split(", ").length, n, kind);
    assert.equal(v["--pv-backdrop-attach"]!.split(", ").length, n, kind);
  }
});

test("dots follow the text colour unless given one", () => {
  const v = backdropVars({ kind: "dots", opacity: 0.5, scale: 20 }, { text: "#102030" });
  assert.match(v["--pv-backdrop"]!, /rgba\(16, 32, 48, 0\.5\)/);
  const own = backdropVars({ kind: "dots", color: "#ff0000", opacity: 0.5, scale: 20 }, { text: "#102030" });
  assert.match(own["--pv-backdrop"]!, /rgba\(255, 0, 0, 0\.5\)/);
});

test("a colour that is not #rrggbb never reaches the CSS", () => {
  const v = backdropVars({ kind: "dots", color: "red;} body{display:none", opacity: 0.5, scale: 20 }, { text: "#102030" });
  assert.doesNotMatch(v["--pv-backdrop"]!, /display|red/);
  assert.match(v["--pv-backdrop"]!, /rgba\(16, 32, 48/);
});

test("numbers are clamped to their ranges", () => {
  const v = backdropVars({ kind: "dots", opacity: 5, scale: 9999 }, {});
  assert.match(v["--pv-backdrop"]!, /, 0\.6\)/);
  assert.equal(v["--pv-backdrop-size"], "200px 200px, 200px 200px");
});

test("fade adds a layer on top; fixed pins every layer", () => {
  const v = backdropVars({ kind: "grid", opacity: 0.2, scale: 24, fade: true, fixed: true }, {});
  assert.equal(layerCount(v), 3);
  assert.match(v["--pv-backdrop"]!, /^linear-gradient\(to bottom, transparent 30%, var\(--bg\) 92%\)/);
  assert.equal(v["--pv-backdrop-attach"], "fixed, fixed, fixed");
});

test("motion drifts only the kinds that tile", () => {
  assert.ok(backdropVars({ kind: "waves", opacity: 0.2, scale: 24, motion: true }, {})["--pv-backdrop-anim"]);
  assert.equal(backdropVars({ kind: "sunburst", opacity: 0.2, scale: 24, motion: true }, {})["--pv-backdrop-anim"], undefined);
});

test("confetti and gradients take the accent and titles colours when none is set", () => {
  const v = backdropVars({ kind: "gradient", opacity: 0.5, scale: 24 }, { accent: "#ff0000", accent_secondary: "#0000ff", text: "#000000" });
  assert.match(v["--pv-backdrop"]!, /rgba\(255, 0, 0, 0\.5\), rgba\(0, 0, 255, 0\.5\)/);
});

test("a step theme keeps the pattern but drops colours picked by hand", () => {
  const themed = withTheme({ text: "#000000", backdrop: { kind: "dots", color: "#ff0000", opacity: 0.3, scale: 20 } }, "night");
  assert.equal(themed?.backdrop?.kind, "dots");
  assert.equal(themed?.backdrop?.color, undefined);
  assert.match(brandVars(themed)["--pv-backdrop"]!, /rgba\(217, 222, 245, 0\.3\)/);
});

test("a saved theme's look replaces the palette whole", () => {
  const look: play.Brand = { background: "#000000", font: "chewy" };
  assert.deepEqual(withTheme({ background: "#ffffff", accent: "#ff0000", backdrop: { kind: "dots", opacity: 0.2, scale: 20 } }, "my-theme", look), look);
});

test("colour spots: one soft layer per spot, palette colours when none is given", () => {
  const v = backdropVars({ kind: "spots", opacity: 1, scale: 50, points: [{ x: 10, y: 20 }, { x: 90, y: 80, color: "#ff0000" }] }, { accent: "#00ff00", text: "#000000" });
  assert.equal(layerCount(v), 2);
  assert.match(v["--pv-backdrop"]!, /radial-gradient\(circle 150px at 10% 20%, rgba\(0, 255, 0, 1\), rgba\(0, 255, 0, 0\)\)/);
  assert.match(v["--pv-backdrop"]!, /at 90% 80%, rgba\(255, 0, 0, 1\)/);
  assert.equal(v["--pv-backdrop-repeat"], "no-repeat, no-repeat");
});

test("only spots may reach full colour", () => {
  assert.match(backdropVars({ kind: "dots", opacity: 1, scale: 20 }, {})["--pv-backdrop"]!, /, 0\.6\)/);
});

test("see-through cards thin the card's fill, and frost it on request", () => {
  const v = brandVars({ background: "#ffffff", style: { card_opacity: 0.6, card_blur: true } });
  assert.equal(v["--pv-card-bg"], "color-mix(in srgb, #ffffff 60%, transparent)");
  assert.equal(v["--pv-card-blur"], "blur(10px)");
  const solid = brandVars({ background: "#ffffff", style: { card_opacity: 1, card_blur: true } });
  assert.equal(solid["--pv-card-bg"], undefined, "a solid card is left alone");
  assert.equal(solid["--pv-card-blur"], undefined, "nothing to frost behind a solid card");
});

test("contrast and blending", () => {
  assert.equal(Math.round(contrastRatio("#000000", "#ffffff")), 21);
  assert.equal(contrastRatio("#777777", "#777777"), 1);
  assert.equal(blend("#000000", "#ffffff", 0.5), "#808080");
});

test("a colour list makes rainbow stripes and party confetti", () => {
  const stripes = backdropVars({ kind: "stripes", opacity: 0.5, scale: 20, colors: ["#ff0000", "#00ff00", "#0000ff"] }, {});
  assert.match(stripes["--pv-backdrop"]!, /rgba\(255, 0, 0, 0\.5\) 0px 10px, rgba\(0, 255, 0, 0\.5\) 10px 20px, rgba\(0, 0, 255, 0\.5\) 20px 30px/);
  const confetti = backdropVars({ kind: "confetti", opacity: 0.5, scale: 20, colors: ["#ff0000", "#00ff00", "#0000ff"] }, {});
  const svg = decodeURIComponent(confetti["--pv-backdrop"]!);
  for (const c of ["#ff0000", "#00ff00", "#0000ff"]) assert.ok(svg.includes(c), c);
});

test("a horizon is one scene: its sky colours, sun and grid, never stretched", () => {
  const v = backdropVars({ kind: "horizon", opacity: 1, scale: 40, color: "#2de2e6", color2: "#ffd319", colors: ["#1c0e31", "#59255b", "#d74789"] }, {});
  assert.equal(layerCount(v), 1);
  const svg = decodeURIComponent(v["--pv-backdrop"]!);
  for (const c of ["#1c0e31", "#59255b", "#d74789", "#2de2e6", "#ffd319"]) assert.ok(svg.includes(c), c);
  assert.ok(svg.includes('preserveAspectRatio="xMidYMid slice"'));
});

test("lined paper: rules every Scale pixels and a margin line", () => {
  const v = backdropVars({ kind: "lined", opacity: 0.2, scale: 30, color: "#5b8fd4", color2: "#e57373" }, {});
  assert.equal(layerCount(v), 2);
  assert.match(v["--pv-backdrop-size"]!, /100% 30px/);
  assert.match(v["--pv-backdrop"]!, /rgba\(229, 115, 115, 0\.5\) 2\.25rem/);
});

test("a picture: its own layer's image, strength, blur, fade and stillness, and a plain background for thumbnails", () => {
  const v = backdropVars({ kind: "picture", media: 42, opacity: 0.7, scale: 100, blur: 8, fade: true, fixed: true }, {});
  assert.equal(v["--pv-photo"], 'url("/media/42")');
  assert.equal(v["--pv-photo-opacity"], "0.7");
  assert.equal(v["--pv-photo-filter"], "blur(8px)");
  assert.equal(v["--pv-photo-position"], "fixed");
  assert.match(v["--pv-photo-mask"]!, /linear-gradient/);
  assert.equal(v["--pv-backdrop"], 'url("/media/42")');
  assert.deepEqual(backdropVars({ kind: "picture", opacity: 1, scale: 100 }, {}), {}, "no image, nothing to paint");
  assert.deepEqual(backdropVars({ kind: "picture", media: "1);x" as never, opacity: 1, scale: 100 }, {}), {}, "only a number reaches the URL");
});

test("text size, button corners and a ribbon title's text", () => {
  const v = brandVars({ accent: "#ffd319", style: { text_scale: 1.2, button_shape: "square" } });
  assert.equal(v["--text-base"], "1.2rem");
  assert.equal(v["--pv-text-scale"], "1.2");
  assert.equal(v["--pv-btn-radius"], "3px");
  assert.equal(v["--pv-on-titles"], "#000000", "dark text on a yellow ribbon");
  assert.equal(brandVars({ style: { text_scale: 9 } })["--text-base"], "1.4rem", "bounded");
});
