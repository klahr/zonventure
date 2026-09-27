// The arithmetic behind the newer blocks: Morse flashing, shaking,
// bearings, votes, spot-the-difference, letter tiles, suspect boards and
// the Caesar wheel. Pure, so it runs under node --test without a phone.

import { ALPHABET, MORSE } from "./ciphers.ts";

/** The punctuation a flasher sends, beyond letters and digits (graph/valid's morseable). */
const PUNCT: Record<string, string> = {
  ".": "·−·−·−", ",": "−−··−−", "?": "··−−··", "'": "·−−−−·", "!": "−·−·−−", "/": "−··−·",
  "(": "−·−−·", ")": "−·−−·−", "&": "·−···", ":": "−−−···", ";": "−·−·−·", "=": "−···−",
  "+": "·−·−·", "-": "−····−", _: "··−−·−", '"': "·−··−·", $: "···−··−", "@": "·−−·−·",
};

/** One stretch of a flash: light on or off, for ms. */
export interface Flash {
  on: boolean;
  ms: number;
}

/** A dot's length at a pace, in ms: the PARIS standard, 1200 / wpm. */
export function morseUnit(wpm: number | undefined): number {
  const pace = wpm && wpm > 0 ? wpm : 8;
  // Never faster than 3 Hz: an on and an off of one unit each take at least a third of a second.
  return Math.max(1000 / 6, 1200 / pace);
}

/**
 * The text as Morse flashes: a dot one unit on, a dash three, one unit
 * dark between them, three between letters and seven between words.
 * Characters Morse has no code for are skipped.
 */
export function morseFlashes(text: string, wpm?: number): Flash[] {
  const u = morseUnit(wpm);
  const out: Flash[] = [];
  // The dark before the next flash, the longest gap asked since the last
  // one: it is only laid down when a flash follows.
  let gap = 0;
  for (const word of text.toUpperCase().split(/\s+/).filter(Boolean)) {
    gap = Math.max(gap, 7);
    for (const ch of word) {
      const code = MORSE[ch] ?? PUNCT[ch];
      if (!code) continue;
      gap = Math.max(gap, 3);
      for (const sym of code) {
        if (out.length) out.push({ on: false, ms: gap * u });
        out.push({ on: true, ms: (sym === "−" ? 3 : 1) * u });
        gap = 1;
      }
    }
  }
  return out;
}

/** The flashes as a navigator.vibrate pattern: buzz, pause, buzz… */
export function vibratePattern(flashes: Flash[]): number[] {
  const out: number[] = [];
  for (const f of flashes) {
    // The pattern starts with a buzz; a leading pause is dropped.
    if (out.length === 0 && !f.on) continue;
    out.push(Math.round(f.ms));
  }
  return out;
}

/** Gravity, which a phone's accelerationIncludingGravity carries at rest. */
const G = 9.81;

/**
 * How hard the phone is being shaken, in m/s² beyond gravity. A phone
 * that reports acceleration without gravity is read as it is; one that
 * reports it only with gravity has g taken off the magnitude.
 */
export function shakeForce(x: number, y: number, z: number, withGravity: boolean): number {
  const m = Math.hypot(x, y, z);
  return withGravity ? Math.abs(m - G) : m;
}

/** The force a shake block's threshold, 1–100, asks for: 60 is a firm shake. */
export function shakeTarget(threshold: number | undefined): number {
  const t = Math.min(100, Math.max(1, threshold ?? 60));
  return 8 + t * 0.22;
}

/** How far a heading is from a target, in degrees, 0–180 either way round. */
export function headingOff(heading: number, target: number): number {
  const d = (((heading - target) % 360) + 360) % 360;
  return d > 180 ? 360 - d : d;
}

/** The option with the most votes, or -1 when none has any or two tie for the lead. */
export function majority(tally: readonly number[] | undefined): number {
  let best = -1;
  let top = 0;
  let tie = false;
  (tally ?? []).forEach((n, i) => {
    if (n > top) {
      top = n;
      best = i;
      tie = false;
    } else if (n === top && n > 0) tie = true;
  });
  return tie ? -1 : best;
}

/** The spot within reach of a tap, as fractions of the picture: the nearest within r, or -1. */
export function nearSpot(p: { x: number; y: number }, spots: readonly { x: number; y: number }[], r = 0.07): number {
  let best = -1;
  let dist = r;
  spots.forEach((s, i) => {
    const d = Math.hypot(s.x - p.x, s.y - p.y);
    if (d <= dist) {
      dist = d;
      best = i;
    }
  });
  return best;
}

/** A suspect board's struck cards, from "0,2". */
export function struckCards(picked: string | undefined): number[] {
  return (picked ?? "")
    .split(",")
    .map((s) => Number.parseInt(s, 10))
    .filter((n) => Number.isInteger(n) && n >= 0);
}

/** The struck list with card i struck or unstruck, as the server keeps it: sorted, comma-separated. */
export function toggleCard(picked: string | undefined, i: number): string {
  const set = new Set(struckCards(picked));
  if (set.has(i)) set.delete(i);
  else set.add(i);
  return [...set].sort((a, b) => a - b).join(",");
}

/** A letters block's tiles: every letter, uppercased, spaces dropped. */
export function letterTiles(text: string): string[] {
  return [...text.toUpperCase()].filter((c) => c.trim() !== "");
}

/** The tiles with one moved from `from` to `to`. */
export function moveTile<T>(tiles: readonly T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= tiles.length || to >= tiles.length) return tiles.slice();
  const out = tiles.slice();
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x!);
  return out;
}

/** The tiles shuffled, differently from how they lay if they can be. */
export function shuffleTiles<T>(tiles: readonly T[], rand: () => number = Math.random): T[] {
  const out = tiles.slice();
  for (let tries = 0; tries < 5; tries++) {
    for (let i = out.length - 1; i > 0; i--) {
      const j = Math.floor(rand() * (i + 1));
      [out[i], out[j]] = [out[j]!, out[i]!];
    }
    if (out.some((x, i) => x !== tiles[i])) break;
  }
  return out;
}

/** One letter's share of the Caesar wheel, in degrees. */
export const WHEEL_STEP = 360 / ALPHABET.length;

/** The shift an inner ring turned by `angle` degrees shows, 0–25. */
export function wheelShift(angle: number): number {
  const n = ALPHABET.length;
  return ((Math.round(angle / WHEEL_STEP) % n) + n) % n;
}
