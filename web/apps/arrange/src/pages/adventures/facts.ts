import type { I18n } from "@zonventure/i18n";
import type { AdventureDetail, Zone } from "./api.ts";

export function haversineKm(lat1: number, lng1: number, lat2: number, lng2: number): number {
  if (!Number.isFinite(lat1) || !Number.isFinite(lat2)) return 0;
  const R = 6371;
  const rad = Math.PI / 180;
  const dLat = (lat2 - lat1) * rad;
  const dLng = (lng2 - lng1) * rad;
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(dLng / 2) ** 2;
  return 2 * R * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

export function formatKm(km: number, locale?: string): string {
  if (km < 1) return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(Math.round(km * 1000)) + " m";
  const digits = km >= 10 ? 0 : 1;
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(km) + " km";
}

export function hintDelay(t: I18n, sec: number): string {
  if (sec <= 0) return t.t("immediate");
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  if (!m) return t.t("after {{s}}s", { s });
  if (!s) return t.t("after {{m}}m", { m });
  return t.t("after {{m}}m {{s}}s", { m, s });
}

/** A place on the adventure's map: a graph step's zone. */
export interface Pin {
  title: string;
  lat: number;
  lng: number;
  radius_m?: number;
  gated: boolean;
  zone?: Zone;
}

/** What the catalog's facts are computed over — one per puzzle. */
export interface FactPage {
  gated: boolean;
  location: { lat: number; lng: number } | null;
  images: number;
  audio: number;
  hints: number[];
}

/** The puzzles an adventure plays: its graph's puzzle steps (a zone is a gate). */
export function factPages(a: AdventureDetail): FactPage[] {
  const g = a.graph;
  if (g) {
    const zones = new Map(g.zones.map((z) => [z.slug, z]));
    return g.steps
      .filter((s) => s.type === "puzzle")
      .map((s) => {
        const z = s.zone ? zones.get(s.zone) : undefined;
        return { gated: !!z, location: z ? { lat: z.lat, lng: z.lng } : null, images: s.pictures ?? 0, audio: s.sounds ?? 0, hints: Array<number>(s.hints ?? 0).fill(0) };
      });
  }
  return [];
}

export interface Facts {
  geo: { kind: "anywhere" | "spot" | "area" | "tour"; shortLabel: string; blurb: string; points: { lat: number; lng: number }[] };
  audioPages: number;
  imagePages: number;
  mediaLabel: string;
  hintsLabel: string;
  playtime: string;
}

/**
 * The decision-relevant shape of an adventure for the catalog: where it
 * can be played, how long it takes, what media and hints it has —
 * without showing a single puzzle.
 */
export function computeFacts(t: I18n, pages: FactPage[]): Facts {
  const gated = pages.filter((p) => p.gated);
  const audioPages = pages.filter((p) => p.audio > 0).length;
  const imagePages = pages.filter((p) => p.images > 0).length;
  const hints = pages.flatMap((p) => p.hints);
  const timedHints = hints.filter((d) => d > 0).length;

  // ~5 minutes a puzzle, a minute more for each with media, in 5-minute buckets.
  const base = pages.length * 5 + audioPages + imagePages;
  const lo = Math.max(15, Math.round((base * 0.8) / 5) * 5);
  const hi = Math.max(lo + 10, Math.round((base * 1.4) / 5) * 5);
  const playtime = t.t("{{lo}}–{{hi}} min", { lo, hi });

  let geo: Facts["geo"];
  if (gated.length === 0) {
    geo = {
      kind: "anywhere",
      shortLabel: t.t("Anywhere"),
      blurb: t.t("No GPS gates — players answer text, image, and audio puzzles wherever you choose to host the event. Adapt prompts to your venue and run it indoors, outdoors, or fully online."),
      points: [],
    };
  } else {
    let minLat = Infinity, maxLat = -Infinity, minLng = Infinity, maxLng = -Infinity;
    const points: { lat: number; lng: number }[] = [];
    for (const p of gated) {
      if (!p.location) continue;
      const { lat, lng } = p.location;
      minLat = Math.min(minLat, lat); maxLat = Math.max(maxLat, lat);
      minLng = Math.min(minLng, lng); maxLng = Math.max(maxLng, lng);
      points.push({ lat, lng });
    }
    const span = points.length ? haversineKm(minLat, minLng, maxLat, maxLng) : 0;
    let kind: Facts["geo"]["kind"], shortLabel: string, blurb: string;
    if (span < 0.4) {
      kind = "spot";
      shortLabel = t.t("Single venue");
      blurb = t.t("All gated puzzles cluster within a ~{{m}} m area — runs as a single-venue event (one park, one campus, one café crawl).", { m: Math.max(50, Math.round(span * 1000)) });
    } else if (span < 5) {
      kind = "area";
      shortLabel = t.t("Specific area (~{{km}} km)", { km: span.toFixed(1) });
      blurb = t.t("Gated puzzles span ~{{km}} km — fits a campus, neighbourhood, or city quarter. Bound to this geography unless you re-author the locations.", { km: span.toFixed(1) });
    } else {
      kind = "tour";
      shortLabel = t.t("Multi-stop tour (~{{km}} km)", { km: Math.round(span) });
      blurb = t.t("Gated puzzles span ~{{km}} km — a city- or region-wide tour. Plan transport for the team between stops.", { km: Math.round(span) });
    }
    if (gated.length < pages.length) {
      const free = pages.length - gated.length;
      blurb += " " + t.plural(free, { one: t.t("{{n}} puzzle is NOT gated and could be re-themed for any venue."), other: t.t("{{n}} puzzles are NOT gated and could be re-themed for any venue.") });
    }
    geo = { kind, shortLabel, blurb, points };
  }

  const media: string[] = [];
  if (imagePages > 0) media.push(t.t("{{n}} with images", { n: imagePages }));
  if (audioPages > 0) media.push(t.t("{{n}} with audio", { n: audioPages }));
  const mediaLabel = media.length === 0 ? t.t("Text-only") : media.join(" · ");

  let hintsLabel: string;
  if (hints.length === 0) hintsLabel = t.t("No hints");
  else {
    const n = t.plural(hints.length, { one: t.t("{{n}} hint"), other: t.t("{{n}} hints") });
    if (timedHints === 0) hintsLabel = hints.length === 1 ? t.t("{{hints}}, immediate", { hints: n }) : t.t("{{hints}}, all immediate", { hints: n });
    else if (timedHints === hints.length) hintsLabel = hints.length === 1 ? t.t("{{hints}}, timed", { hints: n }) : t.t("{{hints}}, all timed", { hints: n });
    else hintsLabel = t.t("{{hints}} ({{m}} timed)", { hints: n, m: timedHints });
  }

  return { geo, audioPages, imagePages, mediaLabel, hintsLabel, playtime };
}

/** Pins for the adventure map: each graph step that happens in a zone. */
export function pinsOf(a: AdventureDetail): Pin[] {
  const g = a.graph;
  if (g) {
    const zones = new Map(g.zones.map((z) => [z.slug, z]));
    return g.steps.flatMap((s) => {
      const z = s.zone ? zones.get(s.zone) : undefined;
      return z ? [{ title: s.title || z.name || s.slug, lat: z.lat, lng: z.lng, radius_m: z.radius_m, gated: true, zone: z }] : [];
    });
  }
  return [];
}

/** The widest distance between two pins: "how big is it on the ground". */
export function extentKm(pins: { lat: number; lng: number }[]): number {
  let max = 0;
  for (let i = 0; i < pins.length; i++)
    for (let j = i + 1; j < pins.length; j++) max = Math.max(max, haversineKm(pins[i]!.lat, pins[i]!.lng, pins[j]!.lat, pins[j]!.lng));
  return max;
}
