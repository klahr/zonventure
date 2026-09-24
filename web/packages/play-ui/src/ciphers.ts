/**
 * The sheets a cipher block shows, as data: what each letter looks like
 * in morse, pigpen and semaphore, a Caesar alphabet, and a custom key.
 * Drawing is the renderer's; this file stays free of React and CSS so
 * node can test it.
 */

export const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const MORSE: Record<string, string> = {
  A: "·−", B: "−···", C: "−·−·", D: "−··", E: "·", F: "··−·", G: "−−·", H: "····", I: "··",
  J: "·−−−", K: "−·−", L: "·−··", M: "−−", N: "−·", O: "−−−", P: "·−−·", Q: "−−·−", R: "·−·",
  S: "···", T: "−", U: "··−", V: "···−", W: "·−−", X: "−··−", Y: "−·−−", Z: "−−··",
  "0": "−−−−−", "1": "·−−−−", "2": "··−−−", "3": "···−−", "4": "····−",
  "5": "·····", "6": "−····", "7": "−−···", "8": "−−−··", "9": "−−−−·",
};

/** A line segment in a unit square, y down. */
export type Segment = [x1: number, y1: number, x2: number, y2: number];

export interface PigpenGlyph {
  segments: Segment[];
  /** Where the dot sits, for the second grid and the second X. */
  dot: [x: number, y: number] | null;
}

/**
 * The standard pigpen: A–I in a plain tic-tac-toe grid, J–R in a dotted
 * one, S–V in a plain X and W–Z in a dotted one. A letter is drawn as
 * the lines of the grid that enclose its cell.
 */
export function pigpen(letter: string): PigpenGlyph | null {
  const i = ALPHABET.indexOf(letter.toUpperCase());
  if (i < 0) return null;
  if (i < 18) {
    const cell = i % 9;
    const row = Math.floor(cell / 3);
    const col = cell % 3;
    const segments: Segment[] = [];
    // A cell is walled on every side that faces another cell.
    if (row > 0) segments.push([0, 0, 1, 0]);
    if (row < 2) segments.push([0, 1, 1, 1]);
    if (col > 0) segments.push([0, 0, 0, 1]);
    if (col < 2) segments.push([1, 0, 1, 1]);
    return { segments, dot: i >= 9 ? [0.5, 0.5] : null };
  }
  // The X's wedges, top, left, right, bottom: each drawn as the two
  // diagonals that meet at the X's middle.
  const wedge = (i - 18) % 4;
  const dotted = i >= 22;
  const shapes: { segments: Segment[]; dot: [number, number] }[] = [
    { segments: [[0, 0, 0.5, 1], [0.5, 1, 1, 0]], dot: [0.5, 0.3] },
    { segments: [[0, 0, 1, 0.5], [1, 0.5, 0, 1]], dot: [0.3, 0.5] },
    { segments: [[1, 0, 0, 0.5], [0, 0.5, 1, 1]], dot: [0.7, 0.5] },
    { segments: [[0, 1, 0.5, 0], [0.5, 0, 1, 1]], dot: [0.5, 0.7] },
  ];
  const s = shapes[wedge]!;
  return { segments: s.segments, dot: dotted ? s.dot : null };
}

/**
 * Semaphore arm positions, as the reader faces the signaller, in steps
 * of 45° clockwise from straight down: 0 down, 2 out to the reader's
 * left, 4 up, 6 out to the right. The standard chart: A–G move one arm
 * round while the other rests down, then each later circle starts from
 * the next position.
 */
export const SEMAPHORE: Record<string, [number, number]> = {
  A: [0, 1], B: [0, 2], C: [0, 3], D: [0, 4], E: [0, 5], F: [0, 6], G: [0, 7],
  H: [1, 2], I: [1, 3], K: [1, 4], L: [1, 5], M: [1, 6], N: [1, 7],
  O: [2, 3], P: [2, 4], Q: [2, 5], R: [2, 6], S: [2, 7],
  T: [3, 4], U: [3, 5], Y: [3, 6],
  J: [4, 6], V: [4, 7], W: [5, 6], X: [5, 7], Z: [6, 7],
};

/** Which way an arm at a semaphore position points, as a unit vector, y down. */
export function armVector(position: number): [dx: number, dy: number] {
  const a = (position * Math.PI) / 4;
  // Round off the float noise so the SVG reads 0, not 6e-17.
  const r = (n: number) => Math.round(n * 1e6) / 1e6 + 0;
  return [r(-Math.sin(a)), r(Math.cos(a))];
}

/** A shift the sheet can show: whole, 1–25, else the classic 3. */
export function caesarShift(shift: number | undefined): number {
  const n = Math.round(shift ?? 3);
  return n >= 1 && n <= 25 ? n : 3;
}

/** The cipher alphabet under A–Z for a shift: 3 puts D under A. */
export function caesarAlphabet(shift: number): string {
  const n = ((shift % 26) + 26) % 26;
  return ALPHABET.slice(n) + ALPHABET.slice(0, n);
}

/** Shifts the letters of a text, keeping case and everything else. */
export function caesar(text: string, shift: number): string {
  const n = ((shift % 26) + 26) % 26;
  return text.replace(/[a-z]/gi, (c) => {
    const base = c <= "Z" ? 65 : 97;
    return String.fromCharCode(((c.charCodeAt(0) - base + n) % 26) + base);
  });
}

export type KeyLine = { symbol: string; meaning: string } | { note: string };

/**
 * A custom key, one "symbol = meaning" per line. Only the first "="
 * splits, so a meaning may hold one; a line without any is a note.
 */
export function parseKey(key: string | undefined): KeyLine[] {
  const out: KeyLine[] = [];
  for (const raw of (key ?? "").split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const at = line.indexOf("=");
    if (at < 0) {
      out.push({ note: line });
      continue;
    }
    out.push({ symbol: line.slice(0, at).trim(), meaning: line.slice(at + 1).trim() });
  }
  return out;
}
