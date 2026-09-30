import type { Graph, Zone } from "@zonventure/graph";

/** A zone's name for display, or its slug when it has none. */
export function zoneName(graph: Graph, slug: string): string {
  return (graph.zones ?? []).find((z) => z.slug === slug)?.name || slug;
}

/** Central Stockholm: where a map opens with no place to show. */
export const DEFAULT_CENTRE = { lat: 59.3293, lng: 18.0686 };

/** Where a new place goes: beside the adventure's last one. */
export function nearLastZone(zones: Zone[]): { lat: number; lng: number } {
  const near = zones[zones.length - 1];
  return near ? { lat: near.lat, lng: near.lng } : DEFAULT_CENTRE;
}

/** The zone groups in use, such as a search grid's, in first-use order. */
export function zoneGroups(zones: Zone[]): string[] {
  return [...new Set(zones.map((z) => z.group).filter((g): g is string => !!g))];
}

/** Where the phone is, with how sure it is, in metres. */
export interface Here {
  lat: number;
  lng: number;
  accuracy: number;
}

/**
 * Asks the phone where it is, once, as precisely as it can. Rejects with a
 * message fit to show when location is off, refused or too slow.
 */
export function locate(t: { t: (s: string) => string }): Promise<Here> {
  return new Promise((resolve, reject) => {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      reject(new Error(t.t("This browser can't tell where you are.")));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (p) => resolve({ lat: p.coords.latitude, lng: p.coords.longitude, accuracy: Math.max(1, Math.round(p.coords.accuracy)) }),
      (e) => reject(new Error(locationError(e, t))),
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 5000 },
    );
  });
}

/** What to tell the author when the phone won't say where it is. */
export function locationError(e: GeolocationPositionError, t: { t: (s: string) => string }): string {
  return e.code === e.PERMISSION_DENIED
    ? t.t("Location is blocked. Allow it for this site in the browser's settings.")
    : e.code === e.TIMEOUT
      ? t.t("Finding your location took too long. Try again outside, away from tall buildings.")
      : t.t("Your location isn't available right now.");
}

/** A new circle zone at `here`, named `name`, with a slug no other zone has. */
export function zoneAt(zones: Zone[], name: string, here: { lat: number; lng: number }, radius_m = 30): Zone {
  const base =
    name
      .toLowerCase()
      .normalize("NFD")
      .replace(/[̀-ͯ]/g, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 40) || "place";
  const taken = new Set(zones.map((z) => z.slug));
  let slug = base;
  for (let i = 2; taken.has(slug); i++) slug = `${base}-${i}`;
  return { slug, name, lat: here.lat, lng: here.lng, radius_m };
}
