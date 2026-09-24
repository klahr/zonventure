/** A point on a picture, as fractions (0–1) of its width and height. */
export interface Fraction {
  x: number;
  y: number;
}

const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

/** Four decimals is a tenth of a pixel on a 1000 px picture, and a stable diff. */
export const round4 = (n: number) => Math.round(n * 1e4) / 1e4;

/** Where a click or touch fell on a picture's box, clamped to the picture. */
export function fractionAt(clientX: number, clientY: number, box: { left: number; top: number; width: number; height: number }): Fraction {
  if (box.width <= 0 || box.height <= 0) return { x: 0.5, y: 0.5 };
  return { x: round4(clamp01((clientX - box.left) / box.width)), y: round4(clamp01((clientY - box.top) / box.height)) };
}

/** How far one arrow-key press moves the torch. */
export const TORCH_STEP = 0.05;

/** The torch after an arrow key, or null for any other key. */
export function nudge(p: Fraction, key: string, step = TORCH_STEP): Fraction | null {
  const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[key];
  if (!d) return null;
  return { x: clamp01(p.x + d[0]! * step), y: clamp01(p.y + d[1]! * step) };
}

/**
 * Which way a spot's caption opens so it stays on the picture: above a
 * spot in the lower half, below one in the upper; hugging the near edge
 * for a spot at either side, centred otherwise.
 */
export function bubbleSide(s: Fraction): { v: "above" | "below"; h: "start" | "middle" | "end" } {
  return { v: s.y > 0.5 ? "above" : "below", h: s.x < 1 / 3 ? "start" : s.x > 2 / 3 ? "end" : "middle" };
}
