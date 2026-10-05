import type { play } from "@zonventure/api";

/**
 * Starting points for a backdrop: picking one fills in every control,
 * which the author then adjusts. Only the values are saved, never the
 * preset, so changing a preset here leaves existing adventures alone.
 * Colours are left empty so each preset follows the adventure's palette.
 */
export const BACKDROP_PRESETS: { id: string; label: string; backdrop: play.Backdrop }[] = [
  { id: "dots", label: "Polka dots", backdrop: { kind: "dots", opacity: 0.08, scale: 28 } },
  { id: "stripes", label: "Candy stripes", backdrop: { kind: "stripes", opacity: 0.06, scale: 28, angle: 45 } },
  { id: "checks", label: "Picnic checks", backdrop: { kind: "checks", opacity: 0.06, scale: 40 } },
  { id: "grid", label: "Graph paper", backdrop: { kind: "grid", opacity: 0.12, scale: 24 } },
  { id: "sunburst", label: "Comic sunburst", backdrop: { kind: "sunburst", opacity: 0.08, scale: 60, fade: true } },
  { id: "waves", label: "Waves", backdrop: { kind: "waves", opacity: 0.14, scale: 32 } },
  { id: "zigzag", label: "Zigzag", backdrop: { kind: "zigzag", opacity: 0.1, scale: 34 } },
  { id: "confetti", label: "Party confetti", backdrop: { kind: "confetti", opacity: 0.5, scale: 30 } },
  { id: "gradient", label: "Soft glow", backdrop: { kind: "gradient", opacity: 0.18, scale: 24, angle: 180 } },
  { id: "horizon", label: "Retro horizon", backdrop: { kind: "horizon", opacity: 1, scale: 40, color: "#2de2e6", color2: "#ffd319", fixed: true, colors: ["#1c0e31", "#2e1f56", "#59255b", "#d74789"] } },
  { id: "lined", label: "Lined paper", backdrop: { kind: "lined", color: "#5b8fd4", opacity: 0.22, scale: 30 } },
  { id: "spots", label: "Colour spots", backdrop: { kind: "spots", opacity: 0.35, scale: 90, points: [{ x: 12, y: 8 }, { x: 88, y: 35 }, { x: 25, y: 80 }] } },
];

/** A backdrop with every optional field spelled out, so two can be compared. */
function normal(b: play.Backdrop) {
  return [b.kind, b.color ?? "", b.color2 ?? "", b.opacity, b.scale, b.angle ?? 0, !!b.fade, !!b.fixed, !!b.motion, JSON.stringify(b.points ?? []), JSON.stringify(b.colors ?? []), b.media ?? 0, b.blur ?? 0].join("|");
}

/** The preset a backdrop still matches exactly, if any. */
export function presetOf(b: play.Backdrop | undefined): string | null {
  if (!b) return null;
  const n = normal(b);
  return BACKDROP_PRESETS.find((p) => normal(p.backdrop) === n)?.id ?? null;
}

/** A backdrop being edited: a control that is cleared leaves its field undefined. */
export type BackdropEdit = Pick<play.Backdrop, "kind" | "opacity" | "scale"> & { [K in keyof play.Backdrop]?: play.Backdrop[K] | undefined };

/** A backdrop without its empty and false fields, as it is saved. */
export function tidy(b: BackdropEdit): play.Backdrop {
  const out: play.Backdrop = { kind: b.kind, opacity: b.opacity, scale: b.scale };
  if (b.color) out.color = b.color;
  if (b.color2) out.color2 = b.color2;
  if (b.angle) out.angle = b.angle;
  if (b.fade) out.fade = true;
  if (b.fixed) out.fixed = true;
  if (b.motion) out.motion = true;
  if (b.colors?.length) out.colors = [...b.colors];
  if (b.media) out.media = b.media;
  if (b.blur) out.blur = b.blur;
  if (b.points?.length) out.points = b.points.map((p) => (p.color ? { x: p.x, y: p.y, color: p.color } : { x: p.x, y: p.y }));
  return out;
}
