// The arithmetic behind the sensor blocks: radar, level and noise. Pure,
// so it runs under node --test without a phone.

import { metresBetween } from "./geo.ts";

/** How close the radar says the team is, coldest last. */
export type Band = "there" | "burning" | "hot" | "warmer" | "cold" | "freezing";

export interface Warmth {
  band: Band;
  /** 0 far away, 1 at the zone's edge: drives the pulse and its colour. */
  heat: number;
}

/** GPS is rarely better than this, so a smaller zone still counts as reached from here. */
const MIN_RADIUS = 20;
/** Beyond this the radar is freezing, however big the zone. */
const FAR_M = 2000;

/**
 * Warmth at `d` metres from a zone's centre. Heat falls on a log scale
 * from the zone's edge out to 2 km (or 20 radii for a huge zone), so each
 * halving of the distance warms it by about the same step: 1 km → 500 m
 * feels like 100 m → 50 m.
 */
export function warmth(d: number, radius: number): Warmth {
  const near = Math.max(MIN_RADIUS, radius);
  if (d <= near) return { band: "there", heat: 1 };
  const far = Math.max(FAR_M, near * 20);
  const heat = clamp(1 - Math.log(d / near) / Math.log(far / near), 0, 1);
  const band: Band = heat >= 0.8 ? "burning" : heat >= 0.6 ? "hot" : heat >= 0.4 ? "warmer" : heat >= 0.2 ? "cold" : "freezing";
  return { band, heat };
}

interface Zone {
  lat: number;
  lng: number;
  radius_m: number;
  polygon?: [number, number][];
}

/**
 * Warmth at a position for a zone. A polygon is taken as a circle round
 * its vertices' mean, as wide as their mean distance from it: close
 * enough for "you're there" without a point-in-polygon test.
 */
export function warmthAt(pos: { lat: number; lng: number }, zone: Zone): Warmth {
  const poly = zone.polygon ?? [];
  if (poly.length < 3) return warmth(metresBetween(pos.lat, pos.lng, zone.lat, zone.lng), zone.radius_m);
  const lat = poly.reduce((s, p) => s + p[0], 0) / poly.length;
  const lng = poly.reduce((s, p) => s + p[1], 0) / poly.length;
  const r = poly.reduce((s, p) => s + metresBetween(lat, lng, p[0], p[1]), 0) / poly.length;
  return warmth(metresBetween(pos.lat, pos.lng, lat, lng), r);
}

/**
 * How often the radar beats at a heat, in ms: what radar.css's pulse
 * does, from 2.4 s far away to half a second at the zone's edge, so the
 * beep and the buzz keep time with the dot.
 */
export function beatMs(heat: number): number {
  return Math.round((2.4 - clamp(heat, 0, 1) * 1.9) * 1000);
}

/** The radar's beep at a heat: an octave from A4 far away to A5 at the edge, so warmer also sounds higher. */
export function beepHz(heat: number): number {
  return 440 * 2 ** clamp(heat, 0, 1);
}

/** Root mean square of an analyser's time-domain samples, 0…1. */
export function rms(samples: ArrayLike<number>): number {
  if (samples.length === 0) return 0;
  let sum = 0;
  for (let i = 0; i < samples.length; i++) sum += samples[i]! * samples[i]!;
  return Math.sqrt(sum / samples.length);
}

/** Loudness 0…100 from an RMS level: −60 dBFS (a quiet room) is 0, full scale is 100. */
export function loudness(level: number): number {
  if (level <= 0) return 0;
  const db = 20 * Math.log10(level);
  return clamp(((db + 60) / 60) * 100, 0, 100);
}

const rad = (d: number) => (d * Math.PI) / 180;
const deg = (r: number) => (r * 180) / Math.PI;

/**
 * How far the phone is from lying flat, in degrees, from a
 * deviceorientation beta (front-back) and gamma (left-right).
 */
export function tilt(beta: number, gamma: number): number {
  return deg(Math.acos(clamp(Math.cos(rad(beta)) * Math.cos(rad(gamma)), -1, 1)));
}

/**
 * Where the back camera looks, in degrees above the horizon: −90 lying
 * flat face up, 0 held upright, 90 looking straight up. The camera looks
 * along the screen's normal, whose height is −cos β·cos γ; held upright
 * (γ ≈ 0) that is simply β − 90, but this stays right when the phone is
 * also leant sideways.
 */
export function elevation(beta: number, gamma: number): number {
  return deg(Math.asin(clamp(-Math.cos(rad(beta)) * Math.cos(rad(gamma)), -1, 1)));
}

/**
 * Where a spirit level's bubble sits, each axis −1…1 across `range`
 * degrees of tilt. It floats to the high side: top edge raised (β > 0)
 * sends it up (y < 0), right edge lowered (γ > 0) sends it left.
 */
export function bubble(beta: number, gamma: number, range = 20): { x: number; y: number } {
  let x = -gamma / range;
  let y = -beta / range;
  // Keep it inside the vial: scale back onto the rim, not into a corner.
  const r = Math.hypot(x, y);
  if (r > 1) {
    x /= r;
    y /= r;
  }
  return { x, y };
}

/** A hold detector's state: since when the condition has held, and whether it held long enough. */
export interface Hold {
  since: number | null;
  done: boolean;
}

export const HOLD_START: Hold = { since: null, done: false };

/**
 * One reading into a hold detector: `ok` must stay true for `ms`
 * continuously, measured on `now`. Any miss starts it over; once done it
 * stays done.
 */
export function holdStep(h: Hold, ok: boolean, now: number, ms: number): Hold {
  if (h.done) return h;
  if (!ok) return h.since == null ? h : HOLD_START;
  const since = h.since ?? now;
  return { since, done: now - since >= ms };
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}
