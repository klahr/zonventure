// An adventure's brand palette, turned into CSS custom properties.
//
// Ported from play.html's applyColors / shade / readableOn so an
// adventure an author styled for the old player looks the same in the
// new one. As there, the palette is applied to the play surface only —
// the element PlayView renders — never to the document root, so an
// organiser's colours can't restyle the app chrome around it.
//
// Authors pick colours; they should not have to pick legible ones. Text
// on an accent is chosen by contrast, and hover and gradient shades are
// derived, so any accent an author types produces a readable button.

import type { play } from "@zonventure/api";
import { backdropVars } from "./backdrop.ts";

const HEX = /^#?([0-9a-f]{6})$/i;

/** Whether s is a #rrggbb colour. Anything else is ignored, not trusted. */
export function isHex(s: string | undefined | null): s is string {
  return !!s && HEX.test(s);
}

function rgb(hex: string): [number, number, number] {
  const n = parseInt(HEX.exec(hex)![1]!, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/**
 * Nudge a colour toward black (amount < 0) or white (amount > 0), by the
 * fraction |amount|. A non-hex input comes back unchanged.
 */
export function shade(hex: string, amount: number): string {
  if (!isHex(hex)) return hex;
  const mix = amount < 0 ? 0 : 255;
  const a = Math.min(1, Math.abs(amount));
  return (
    "#" +
    rgb(hex)
      .map((v) => Math.round(v + (mix - v) * a).toString(16).padStart(2, "0"))
      .join("")
  );
}

/** WCAG relative luminance, 0 (black) to 1 (white). */
export function luminance(hex: string): number {
  const [r, g, b] = rgb(hex).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** Black or white, whichever reads better on bg. */
/** WCAG contrast between two #rrggbb colours: 1 (none) to 21 (black on white). */
export function contrastRatio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/** Lay `top` over `under` at the given opacity, as the eye sees it. */
export function blend(under: string, top: string, opacity: number): string {
  const u = rgb(under);
  const t = rgb(top);
  return "#" + u.map((v, i) => Math.round(v + (t[i]! - v) * opacity).toString(16).padStart(2, "0")).join("");
}

export function readableOn(bg: string): "#000000" | "#ffffff" {
  if (!isHex(bg)) return "#ffffff";
  // The crossover where black and white have equal contrast; play.html
  // used 0.5, which leaves mid-tone pinks with dim white text.
  return luminance(bg) > 0.179 ? "#000000" : "#ffffff";
}

/**
 * The typefaces an adventure may choose — the same keys as adventure.Fonts
 * in Go. Self-hosted: @font-face rules in fonts/adventure-fonts.css,
 * which PlayView imports; see fonts/README.md.
 */
/**
 * The default Zonventure look spelled out: tokens.css's light colours,
 * the standard typeface, rounded corners and raised cards. What a new
 * saved theme starts from, so every control shows a real value.
 */
export const DEFAULT_LOOK: play.Brand = {
  accent: "#ad1357",
  accent_secondary: "#ad1357",
  background: "#fbfbfd",
  text: "#1f3349",
  submit_bg: "#fbfbfd",
};

export const FONTS: Record<string, string> = {
  inconsolata: "Inconsolata",
  "special-elite": "Special Elite",
  merriweather: "Merriweather",
  fredoka: "Fredoka",
  "baloo-2": "Baloo 2",
  "patrick-hand": "Patrick Hand",
  chewy: "Chewy",
  "luckiest-guy": "Luckiest Guy",
  nunito: "Nunito",
  inter: "Inter",
  creepster: "Creepster",
  pacifico: "Pacifico",
  vt323: "VT323",
  orbitron: "Orbitron",
  "press-start-2p": "Press Start 2P",
};

/**
 * Corner presets, as the radius tokens every rounded thing reads: soft
 * keeps a hint of round, sharp squares everything but a 2px edge that
 * stops it looking unfinished. Rounded is the tokens' own values.
 */
const CORNERS: Record<string, Record<string, string>> = {
  soft: { "--radius-sm": "3px", "--radius": "6px", "--radius-lg": "8px", "--radius-pill": "10px" },
  sharp: { "--radius-sm": "2px", "--radius": "2px", "--radius-lg": "2px", "--radius-pill": "2px" },
};

/**
 * The themes a step's "Change the theme" effect may switch to. Each
 * overrides the adventure palette's colours and keeps its typefaces and
 * shapes; "default", a name not here, or none, is the adventure's own
 * palette. graph.Themes in Go lists the same names.
 */
export const THEMES: Record<string, play.Brand> = {
  dark: { background: "#14181f", text: "#e8edf3", accent: "#5aa9ff", accent_secondary: "#5aa9ff", submit_bg: "#1f2630" },
  night: { background: "#0b1026", text: "#d9def5", accent: "#9d8cff", accent_secondary: "#9d8cff", submit_bg: "#161c3a" },
  alarm: { background: "#2a0b0b", text: "#ffe9e6", accent: "#ff4d3d", accent_secondary: "#ff4d3d", submit_bg: "#3d1010" },
  parchment: { background: "#f3e7c9", text: "#3b2a17", accent: "#8a4b1d", accent_secondary: "#8a4b1d", submit_bg: "#e4d3a8" },
};

/**
 * A palette with a step's theme laid over it. A saved theme's look (the
 * view's theme_look) replaces the palette whole. A built-in theme swaps
 * the colours; a backdrop keeps its pattern but drops colours picked by
 * hand, so it follows the theme's.
 */
export function withTheme(b: play.Brand | null | undefined, theme: string | undefined, look?: play.Brand | null): play.Brand | null {
  if (look) return look;
  const t = theme ? THEMES[theme] : undefined;
  if (!t) return b ?? null;
  const out: play.Brand = { ...b, ...t };
  if (b?.backdrop) {
    const { color: _c, color2: _c2, ...rest } = b.backdrop;
    out.backdrop = rest;
  }
  return out;
}

/** Start fetching an adventure's typeface before text needs it. Offline and uncached, text falls back to the system font. */
export function loadFont(key: string | undefined) {
  const family = key ? FONTS[key] : undefined;
  if (!family || typeof document === "undefined" || !document.fonts) return;
  void document.fonts.load(`1em "${family}"`).catch(() => {});
}

/** tokens.css's status colours, light and dark. */
const LIGHT_STATUS = { danger: "#b3261e", success: "#1b6b43", warning: "#a15c07" };
const DARK_STATUS = { danger: "#f2837c", success: "#6dcf92", warning: "#f0b45a" };

/**
 * CSS custom properties for a palette. Empty for no palette, so the
 * default Zonventure look applies. Invalid values are dropped: a colour
 * reaches a style attribute only if it is a plain hex triplet.
 */
export function brandVars(b: play.Brand | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  if (!b) return out;
  if (isHex(b.accent)) {
    out["--accent"] = b.accent;
    out["--accent-2"] = shade(b.accent, 0.22);
    out["--accent-deep"] = shade(b.accent, -0.3);
    out["--pv-on-accent"] = readableOn(b.accent);
  }
  const second = isHex(b.accent_secondary) ? b.accent_secondary : isHex(b.accent) ? b.accent : null;
  if (second) out["--pv-accent-2nd"] = second;
  if (isHex(b.background)) {
    out["--bg"] = b.background;
    out["--surface"] = b.background;
    out["--surface-2"] = `color-mix(in srgb, ${b.background} 92%, ${readableOn(b.background)})`;
    out["--border-soft"] = `color-mix(in srgb, ${b.background} 85%, ${readableOn(b.background)})`;
    // Status colours and their boxes follow the page: the tokens' light
    // pair on a light page, their dark pair on a dark one, each box a
    // tint of the page so the author's text colour still reads on it.
    const status = readableOn(b.background) === "#ffffff" ? DARK_STATUS : LIGHT_STATUS;
    for (const [k, v] of Object.entries(status)) {
      out[`--${k}`] = v;
      out[`--${k}-bg`] = `color-mix(in srgb, ${v} 16%, ${b.background})`;
    }
  }
  if (isHex(b.text)) {
    out["--ink"] = b.text;
    out["--ink-soft"] = b.text;
    out["--muted"] = `color-mix(in srgb, ${b.text} 72%, transparent)`;
  }
  const family = b.font ? FONTS[b.font] : undefined;
  if (family) {
    out["--font-body"] = `"${family}", system-ui, sans-serif`;
    out["--font-brand"] = `"${family}", system-ui, sans-serif`;
  }
  const heading = b.heading_font ? FONTS[b.heading_font] : undefined;
  if (heading) out["--font-brand"] = `"${heading}", system-ui, sans-serif`;
  Object.assign(out, CORNERS[b.corners ?? ""] ?? {});
  // Depth and line are separate choices. Flat drops the shadow; without
  // a line it needs a fill, or a card on a page of its own colour would
  // disappear. The line is one the eye can find on any page colour.
  const ink = isHex(b.text) ? b.text : "#1f3349";
  if (b.cards === "flat") {
    out["--shadow"] = "none";
    out["--shadow-sm"] = "none";
    if (!b.card_outline) {
      out["--pv-card-bg"] = `color-mix(in srgb, var(--surface) 93%, ${ink})`;
      out["--pv-card-line"] = "transparent";
    }
  }
  if (b.card_outline) out["--pv-card-line"] = `color-mix(in srgb, ${ink} 30%, transparent)`;
  // Text and titles, larger or smaller together.
  const scale = b.style?.text_scale;
  if (scale && scale !== 1) {
    const k = Math.min(1.4, Math.max(0.85, scale));
    out["--pv-text-scale"] = String(k);
    out["--text-sm"] = `${0.875 * k}rem`;
    out["--text-base"] = `${k}rem`;
    out["--text-lg"] = `${1.15 * k}rem`;
  }
  // Buttons' corners: a pill unless chosen otherwise.
  const shape = b.style?.button_shape;
  if (shape === "rounded") out["--pv-btn-radius"] = "0.75rem";
  else if (shape === "square") out["--pv-btn-radius"] = "3px";
  // Text on a ribbon title: picked for the titles colour, as on the accent.
  const titles = isHex(b.accent_secondary) ? b.accent_secondary : isHex(b.accent) ? b.accent : null;
  if (titles) out["--pv-on-titles"] = readableOn(titles);
  // See-through cards: the card's own fill, thinned, so the backdrop shows.
  const opacity = b.style?.card_opacity;
  if (opacity && opacity < 1) {
    const fill = out["--pv-card-bg"] ?? (isHex(b.background) ? b.background : "var(--surface)");
    out["--pv-card-bg"] = `color-mix(in srgb, ${fill} ${Math.round(Math.max(0.2, opacity) * 100)}%, transparent)`;
    if (b.style?.card_blur) out["--pv-card-blur"] = "blur(10px)";
  }
  if (isHex(b.submit_bg)) {
    out["--pv-dock-bg"] = b.submit_bg;
    out["--pv-dock-fg"] = readableOn(b.submit_bg);
  }
  // The answer button's own colours, over the fill and the bar.
  const answer = b.style?.answer_button_color;
  const answerText = b.style?.answer_button_text_color;
  if (isHex(answer)) out["--pv-answer-bg"] = answer;
  if (isHex(answerText)) out["--pv-answer-fg"] = answerText;
  else if (isHex(answer)) out["--pv-answer-fg"] = readableOn(answer);
  Object.assign(out, backdropVars(b.backdrop, b));
  return out;
}
