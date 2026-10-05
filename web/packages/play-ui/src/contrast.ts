import type { play } from "@zonventure/api";
import { backdropPalette } from "./backdrop.ts";
import { DEFAULT_LOOK, blend, contrastRatio, isHex } from "./brand.ts";

/**
 * Where a look's text may be hard to read outdoors on a phone, worst
 * first. The thresholds are WCAG AA's: 4.5:1 for text, 3:1 for titles,
 * which are large. Text on the accent and on the answer bar is chosen by
 * the player itself (readableOn), so only the author's own pairs count:
 *
 *   - text: the text colour on the page, over the backdrop's strongest
 *     colour too — hints and notes sit on the page itself;
 *   - titles: the titles colour, likewise;
 *   - cards: text on a see-through card, over the backdrop's strongest
 *     colour — the worst a team will meet as it scrolls;
 *   - buttons: an outlined or soft button's label, the accent on the page.
 */
export interface ContrastIssue {
  key: "text" | "titles" | "cards" | "buttons";
  ratio: number;
  min: number;
}

export function lookContrast(b: play.Brand): ContrastIssue[] {
  const hex = (v: string | undefined, fallback: string) => (isHex(v) ? v : fallback);
  const page = hex(b.background, DEFAULT_LOOK.background!);
  const text = hex(b.text, DEFAULT_LOOK.text!);
  const accent = hex(b.accent, DEFAULT_LOOK.accent!);
  const titles = hex(b.accent_secondary, accent);
  const out: ContrastIssue[] = [];
  const check = (key: ContrastIssue["key"], ratio: number, min: number) => {
    if (ratio < min) out.push({ key, ratio: Math.round(ratio * 10) / 10, min });
  };
  // The colours the backdrop paints, as backdrop.ts resolves them.
  const bd = b.backdrop;
  const palette = backdropPalette(b);
  const colourful = bd && (bd.kind === "confetti" || bd.kind === "gradient" || bd.kind === "spots");
  const paints = !bd
    ? []
    : bd.kind === "horizon"
      // Words sit on the sky or the floor; the sun and the glow are a band
      // across the middle, so only the sky above them and the floor count.
      ? (bd.colors && bd.colors.length >= 2 ? bd.colors.slice(0, -1) : ["#1c0e31", "#2e1f56", "#59255b"])
      : bd.kind === "spots"
      ? (bd.points ?? []).map((p, i) => p.color ?? palette[i % palette.length])
      : bd.colors?.length
        ? bd.colors
        : [bd.color ?? (colourful ? palette[0] : text), bd.color2 ?? (colourful ? palette[1] : undefined)];
  // What the page can look like under a word: its colour, or that with
  // the backdrop's strongest paint over it. A fade leaves the bottom
  // plain, but the top, where titles sit, gets the full pattern.
  // Sparse patterns cover little of the page, so a word mostly sits on
  // the page between them: their paint counts for its share of the area.
  const cover = bd ? ({ confetti: 0.3, dots: 0.3, grid: 0.3, waves: 0.4, zigzag: 0.4, lined: 0.15 } as Record<string, number>)[bd.kind] ?? 1 : 1;
  const pages = [page, ...paints.filter(isHex).map((c) => blend(page, c, Math.min(1, bd!.opacity) * cover))];
  const worst = (fg: string, under: string[]) => under.reduce((a, u) => Math.min(a, contrastRatio(fg, u)), Infinity);
  check("text", worst(text, pages), 4.5);
  check("titles", worst(titles, pages), 3);

  // Outlined and soft buttons put the accent's colour on the page itself.
  if (b.style?.button_fill === "outline" || b.style?.button_fill === "soft") check("buttons", worst(accent, pages), 3);

  const opacity = b.style?.card_opacity;
  if (opacity && opacity < 1 && bd) check("cards", worst(text, pages.map((u) => blend(u, page, opacity))), 4.5);
  return out.sort((x, y) => x.ratio / x.min - y.ratio / y.min);
}
