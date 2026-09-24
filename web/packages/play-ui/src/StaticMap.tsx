import { useMemo } from "react";

/** A zone's shape: graph.Zone and play.PlayZone both are one. */
export interface MapZone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
  polygon?: [number, number][];
}

/** A point drawn on the map, such as the team. */
export interface MapPoint {
  lat: number;
  lng: number;
}

/**
 * A small, non-interactive map of a zone: OpenStreetMap tiles placed by
 * Web-Mercator arithmetic, with the circle or polygon drawn over them.
 *
 * No map library and no WebGL, deliberately. A canvas can hold many start
 * cards, and browsers allow only a handful of WebGL contexts per page —
 * one MapLibre map per card would fail there. Plain <img> tiles are cheap,
 * cache like any image, and are all a glance at "where is this" needs.
 * Editing a zone uses the real map (the designer's ZoneMap). Players see
 * the same map in a step's map block.
 *
 * Tiles come from tile.openstreetmap.org, which asks for attribution and
 * light use; a designer with a few authors is that. Swap TILE_URL for a
 * paid or self-hosted source before maps reach every player's phone.
 */
export const TILE_URL = (z: number, x: number, y: number) => `https://tile.openstreetmap.org/${z}/${x}/${y}.png`;

const TILE = 256;

/** Web-Mercator world pixel of a point at zoom z. */
export function project(lat: number, lng: number, z: number): { x: number; y: number } {
  const scale = TILE * 2 ** z;
  const s = Math.sin((Math.max(-85, Math.min(85, lat)) * Math.PI) / 180);
  return {
    x: ((lng + 180) / 360) * scale,
    y: (0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)) * scale,
  };
}

/** Metres per pixel at a latitude and zoom. */
const metresPerPixel = (lat: number, z: number) => (156543.03392 * Math.cos((lat * Math.PI) / 180)) / 2 ** z;

/** The zone's outline as [lat, lng] points: its polygon, or its circle as 48 points. */
function outline(zone: MapZone): [number, number][] {
  if ((zone.polygon?.length ?? 0) >= 3) return zone.polygon!;
  const pts: [number, number][] = [];
  const dLat = zone.radius_m / 111320;
  const dLng = zone.radius_m / (111320 * Math.cos((zone.lat * Math.PI) / 180));
  for (let i = 0; i < 48; i++) {
    const a = (i / 48) * 2 * Math.PI;
    pts.push([zone.lat + dLat * Math.sin(a), zone.lng + dLng * Math.cos(a)]);
  }
  return pts;
}

/** The highest zoom (≤ 18) at which the points fill at most `fill` of the frame. */
function fitZoom(pts: [number, number][], width: number, height: number, fill = 0.7): number {
  for (let z = 18; z > 2; z--) {
    const ps = pts.map(([la, ln]) => project(la, ln, z));
    const w = Math.max(...ps.map((p) => p.x)) - Math.min(...ps.map((p) => p.x));
    const h = Math.max(...ps.map((p) => p.y)) - Math.min(...ps.map((p) => p.y));
    if (w <= width * fill && h <= height * fill) return z;
  }
  return 3;
}

/**
 * `zoom` fixes the zoom level, centred on the zone; markers outside the
 * frame are then cut off. Without it the frame fits the zone and every
 * marker.
 */
export function StaticMap({ zone, width, height, className, markers = [], zoom }: { zone: MapZone; width: number; height: number; className?: string; markers?: MapPoint[]; zoom?: number | undefined }) {
  const m = useMemo(() => {
    const pts = outline(zone);
    const all: [number, number][] = zoom ? pts : [...pts, ...markers.map((p): [number, number] => [p.lat, p.lng])];
    const z = zoom ?? fitZoom(all, width, height);
    const ps = pts.map(([la, ln]) => project(la, ln, z));
    const frame = all.map(([la, ln]) => project(la, ln, z));
    // Centre on the shape itself, which for a polygon is not its lat/lng.
    const cx = (Math.max(...frame.map((p) => p.x)) + Math.min(...frame.map((p) => p.x))) / 2;
    const cy = (Math.max(...frame.map((p) => p.y)) + Math.min(...frame.map((p) => p.y))) / 2;
    const left = cx - width / 2;
    const top = cy - height / 2;
    const n = 2 ** z;
    const tiles: { key: string; src: string; x: number; y: number }[] = [];
    for (let ty = Math.floor(top / TILE); ty <= Math.floor((top + height) / TILE); ty++) {
      if (ty < 0 || ty >= n) continue;
      for (let tx = Math.floor(left / TILE); tx <= Math.floor((left + width) / TILE); tx++) {
        const wx = ((tx % n) + n) % n;
        tiles.push({ key: `${tx}/${ty}`, src: TILE_URL(z, wx, ty), x: tx * TILE - left, y: ty * TILE - top });
      }
    }
    const path = ps.map((p) => `${(p.x - left).toFixed(1)},${(p.y - top).toFixed(1)}`).join(" ");
    const dots = markers.map((p) => {
      const q = project(p.lat, p.lng, z);
      return { x: q.x - left, y: q.y - top };
    });
    return { tiles, path, dots, z, mpp: metresPerPixel(zone.lat, z) };
    // markers is compared by value: a new array each render must not redraw.
  }, [zone, width, height, zoom, JSON.stringify(markers)]);

  return (
    <div className={"static-map" + (className ? ` ${className}` : "")} style={{ width, height }} aria-label={zone.name || zone.slug} role="img">
      {m.tiles.map((t) => (
        <img key={t.key} src={t.src} alt="" draggable={false} loading="lazy" style={{ left: t.x, top: t.y }} />
      ))}
      <svg width={width} height={height} aria-hidden="true">
        <polygon points={m.path} />
        {m.dots.map((d, i) => (
          <circle key={i} className="static-map__dot" cx={d.x} cy={d.y} r={7} />
        ))}
      </svg>
      <span className="static-map__credit">© OpenStreetMap</span>
    </div>
  );
}
