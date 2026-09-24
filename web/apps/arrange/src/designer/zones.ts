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
