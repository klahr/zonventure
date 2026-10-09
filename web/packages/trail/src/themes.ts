import type { CSSProperties } from "react";
import type { play } from "@zonventure/api";
import { FONTS, STARTER_THEMES, backdropVars, loadFont, readableOn, shade } from "@zonventure/play-ui";

/**
 * The looks a party adventure picks from: the starter themes that suit a
 * party, in the order the picker shows them. The look is saved as the
 * adventure's own (PUT …/theme) and dresses the creator's card at once.
 */
export interface TrailTheme {
  id: string;
  label: string;
  look: play.Brand;
}

const PICKS = ["party", "christmas", "halloween", "easter", "midsummer", "summer", "winter", "spring", "autumn", "valentines", "sunshine", "candy", "rainbow", "unicorn", "ice-cream", "jungle", "pirate", "ocean", "space", "detective", "retro-80s", "office", "pen-and-paper"];

export const TRAIL_THEMES: TrailTheme[] = PICKS.flatMap((id) => {
  const s = STARTER_THEMES.find((x) => x.id === id);
  return s ? [{ id: s.id, label: s.label, look: s.look as play.Brand }] : [];
});

/** The look each template starts with. */
export const TEMPLATE_THEMES: Record<string, string> = {
  birthday: "party",
  kids: "jungle",
  office: "office",
  family: "sunshine",
  blank: "rainbow",
};

export const themeById = (id: string | undefined) => TRAIL_THEMES.find((t) => t.id === id);

/**
 * The creator's colours, title font and background from a look: its
 * accent, a gradient from the accent to its second colour (and one of
 * the second colour alone, for information cards), its page colour and
 * text, its backdrop as the player draws it (play-ui's backdropVars, the
 * --pv-backdrop-* properties), and its heading font. Loads the font on
 * the way.
 */
export function themeStyle(look: play.Brand | undefined): CSSProperties {
  if (!look) return {};
  const out: Record<string, string> = {};
  const a = look.accent;
  if (a) {
    const b = look.accent_secondary && look.accent_secondary !== a ? look.accent_secondary : shade(a, -0.35);
    out["--accent"] = a;
    out["--trail-brand"] = `linear-gradient(135deg, ${shade(a, 0.18)}, ${a} 50%, ${b})`;
    out["--trail-info"] = `linear-gradient(135deg, ${shade(b, 0.12)}, ${b} 55%, ${shade(b, -0.32)})`;
    out["--trail-info-solid"] = readableOn(b) === "#ffffff" ? b : shade(b, -0.55);
    out["--trail-on-brand"] = readableOn(a);
    out["--trail-on-info"] = readableOn(b);
  }
  if (look.background) out["--trail-bg"] = look.background;
  if (look.text) out["--trail-ink"] = look.text;
  Object.assign(out, backdropVars(look.backdrop, look));
  const font = look.heading_font ? FONTS[look.heading_font] : undefined;
  if (font) {
    loadFont(look.heading_font);
    out["--trail-heading"] = `"${font}", var(--font-brand)`;
  }
  return out as CSSProperties;
}
