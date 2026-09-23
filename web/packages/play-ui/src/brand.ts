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
export function readableOn(bg: string): "#000000" | "#ffffff" {
  if (!isHex(bg)) return "#ffffff";
  // The crossover where black and white have equal contrast; play.html
  // used 0.5, which leaves mid-tone pinks with dim white text.
  return luminance(bg) > 0.179 ? "#000000" : "#ffffff";
}

/**
 * CSS custom properties for a palette. Empty for no palette, so the
 * default Zonventure look applies. Invalid values are dropped: a colour
 * reaches a style attribute only if it is a plain hex triplet.
 */
/**
 * The typefaces an adventure may choose — the same keys as adventure.Fonts
 * in Go. Loaded from Google Fonts, the only font host the page allows.
 */
export const FONTS: Record<string, string> = {
  inconsolata: "Inconsolata",
  "special-elite": "Special Elite",
  merriweather: "Merriweather",
  fredoka: "Fredoka",
};

/** Load an adventure's typeface once per page. Offline, text falls back to the system font. */
export function loadFont(key: string | undefined) {
  const family = key ? FONTS[key] : undefined;
  if (!family || typeof document === "undefined") return;
  const id = `zv-font-${key}`;
  if (document.getElementById(id)) return;
  const link = document.createElement("link");
  link.id = id;
  link.rel = "stylesheet";
  link.href = `https://fonts.googleapis.com/css2?family=${encodeURIComponent(family).replace(/%20/g, "+")}:wght@400;700&display=swap`;
  document.head.appendChild(link);
}

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
  if (isHex(b.submit_bg)) {
    out["--pv-dock-bg"] = b.submit_bg;
    out["--pv-dock-fg"] = readableOn(b.submit_bg);
  }
  return out;
}
