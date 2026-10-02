import { useMemo } from "react";
import { project, unproject } from "./geo.ts";

export { project, unproject };

/** A zone's shape: graph.Zone and play.PlayZone both are one. */
export interface MapZone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
  polygon?: [number, number][];
}

/** A point on the map, such as the team. */
export interface MapPoint {
  lat: number;
  lng: number;
}

/** A pin an author put on the map, with its label. */
export interface MapPin extends MapPoint {
  label?: string;
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
 * Tiles come from the operator's tile source, which the server hands the
 * page as window.ZV_MAP (/assets/map-config.js, set by ZV_MAP_TILE_URL);
 * a page without it falls back to tile.openstreetmap.org, whose usage
 * policy allows light use only.
 */
const DEFAULT_TILES = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
const mapConfig = (): { tile_url?: string; attribution?: string } | undefined =>
  (globalThis as { ZV_MAP?: { tile_url?: string; attribution?: string } }).ZV_MAP;
export const TILE_URL = (z: number, x: number, y: number) =>
  (mapConfig()?.tile_url || DEFAULT_TILES).replace("{z}", String(z)).replace("{x}", String(x)).replace("{y}", String(y));

/** The tile source's credit as plain text: the configured one is HTML, made for Leaflet. */
function credit(): string {
  const html = mapConfig()?.attribution;
  if (!html || typeof DOMParser === "undefined") return "© OpenStreetMap";
  return new DOMParser().parseFromString(html, "text/html").body.textContent?.trim() || "© OpenStreetMap";
}

const TILE = 256;

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
 * `zoom` fixes the zoom level, centred on the zone; the team and pins
 * outside the frame are then cut off. Without it the frame fits the
 * zone, the pins and the team.
 */
export function StaticMap({ zone, width, height, className, team, pins = [], zoom, onPick }: {
  zone: MapZone;
  width: number;
  height: number;
  className?: string;
  team?: MapPoint | undefined;
  pins?: MapPin[];
  zoom?: number | undefined;
  /** Makes the map clickable: called with the point clicked. */
  onPick?: ((p: MapPoint) => void) | undefined;
}) {
  const m = useMemo(() => {
    const pts = outline(zone);
    const points = [...pins, ...(team ? [team] : [])];
    const all: [number, number][] = zoom ? pts : [...pts, ...points.map((p): [number, number] => [p.lat, p.lng])];
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
    const at = (p: MapPoint) => {
      const q = project(p.lat, p.lng, z);
      return { x: q.x - left, y: q.y - top };
    };
    return { tiles, path, z, left, top, mpp: metresPerPixel(zone.lat, z), team: team && at(team), pins: pins.map((p) => ({ ...at(p), label: p.label })) };
    // team and pins are compared by value: new objects each render must not redraw.
  }, [zone, width, height, zoom, JSON.stringify(team), JSON.stringify(pins)]);

  return (
    <div
      className={"static-map" + (onPick ? " is-pickable" : "") + (className ? ` ${className}` : "")}
      style={{ width, height }}
      aria-label={zone.name || zone.slug}
      role="img"
      onClick={
        onPick &&
        ((e) => {
          const r = e.currentTarget.getBoundingClientRect();
          onPick(unproject(m.left + (e.clientX - r.left) * (width / r.width), m.top + (e.clientY - r.top) * (height / r.height), m.z));
        })
      }
    >
      {m.tiles.map((t) => (
        <img key={t.key} src={t.src} alt="" draggable={false} loading="lazy" style={{ left: t.x, top: t.y }} />
      ))}
      <svg width={width} height={height} aria-hidden="true">
        <polygon points={m.path} />
        {m.pins.map((p, i) => (
          <g key={i} className="static-map__pin" transform={`translate(${p.x.toFixed(1)} ${p.y.toFixed(1)})`}>
            <path d="M0 0 C-7 -10 -9 -14 -9 -18 A9 9 0 1 1 9 -18 C9 -14 7 -10 0 0 Z" />
            <circle cx={0} cy={-18} r={3.5} />
            {p.label && (
              <text x={12} y={-14}>
                {p.label}
              </text>
            )}
          </g>
        ))}
        {m.team && <circle className="static-map__dot" cx={m.team.x} cy={m.team.y} r={7} />}
      </svg>
      <span className="static-map__credit">{credit()}</span>
    </div>
  );
}
