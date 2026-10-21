import { setTranslation, zoneKey, type Graph, type Node, type Zone } from "@zonventure/graph";

/**
 * The graph with a step retitled. The zone the step stands in follows the
 * new title while the zone still has no name, the step's old title, or
 * one of `fallbacks` (what "Mark where I stand" names it for a step
 * without a title): once the author renames the zone, it keeps its own name.
 * In a translation (`translating`, as EditingAs has it), `node` is the step
 * as shown: the zone's name is compared as shown too, and a named zone's
 * new name is its translation, as renameZone makes it — even typed back to
 * the main name. An unnamed zone is named for the Designer to sort out.
 */
export function retitle(graph: Graph, node: Node, title: string, fallbacks: string[] = [], translating?: { lang: string; tr: Record<string, string> }): Graph {
  const tr = translating?.tr;
  const old = node.title ?? "";
  const zoneSlug = node.puzzle?.zone ?? node.story?.zone ?? node.start?.zone;
  const shown = (z: Zone) => (z.group ? undefined : tr?.[zoneKey(z.slug)]?.trim()) || z.name;
  const follows = (z: Zone) => {
    const name = shown(z);
    return z.slug === zoneSlug && (!name || name === old || fallbacks.includes(name));
  };
  const nodes = graph.nodes.map((n) => (n.slug === node.slug ? { ...node, title } : n));
  if (!graph.zones?.some(follows)) return { ...graph, nodes };
  const translated = (z: Zone) => !!translating && !z.group && !!z.name?.trim();
  let out: Graph = { ...graph, nodes, zones: graph.zones.map((z) => (follows(z) && !translated(z) ? { ...z, name: title } : z)) };
  for (const z of graph.zones) if (follows(z) && translated(z)) out = renameZone(out, z, title, translating);
  return out;
}

/**
 * The graph with a zone renamed from a name field. In a translation
 * (`translating`, as EditingAs has it) the name typed is that language's,
 * as setTranslation makes it: emptied, the zone is untranslated again, and
 * typed as the main name, its translation is that name. A zone without a
 * main name keeps none: it is only translated, as the main language's to
 * name. A grouped zone, whose name is not translated, is renamed as
 * outside one.
 */
export function renameZone(graph: Graph, zone: Zone, name: string, translating?: { lang: string }): Graph {
  if (translating && !zone.group) return setTranslation(graph, translating.lang, zoneKey(zone.slug), name, zone.name ?? "");
  return { ...graph, zones: (graph.zones ?? []).map((z) => (z.slug === zone.slug ? { ...z, name } : z)) };
}

/** The name a zone's name field shows: its translation, in one, else its main name. */
export function shownZoneName(zone: Zone, translating?: { tr: Record<string, string> }): string {
  return (translating && !zone.group ? translating.tr[zoneKey(zone.slug)]?.trim() : undefined) || (zone.name ?? "");
}

/** A zone's name for display, or its slug when it has none. */
export function zoneName(graph: Graph, slug: string): string {
  return (graph.zones ?? []).find((z) => z.slug === slug)?.name || slug;
}

/** Where a map opens with no place to show: ZV_MAP_CENTER, else central Stockholm. */
export function defaultCentre(): { lat: number; lng: number } {
  const [lat = NaN, lng = NaN] = ((globalThis as { ZV_MAP?: { center?: string } }).ZV_MAP?.center ?? "").split(",").map(Number);
  return Number.isFinite(lat) && Number.isFinite(lng) ? { lat, lng } : { lat: 59.3293, lng: 18.0686 };
}

/** Where a new place goes: beside the adventure's last one. */
export function nearLastZone(zones: Zone[]): { lat: number; lng: number } {
  const near = zones[zones.length - 1];
  return near ? { lat: near.lat, lng: near.lng } : defaultCentre();
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
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 },
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
