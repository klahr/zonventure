import { useEffect, useRef, useState } from "react";
import type { Zone } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { LocateFixed } from "lucide-react";
import { conceptColor } from "./dz/concepts.tsx";
import { ICON } from "./dz/ui.tsx";
import { isTouch } from "./device.ts";
import { locate } from "./zones.ts";

/**
 * Edit a zone on a real map: drag the centre and set the radius of a
 * circle, or click corners to draw a polygon.
 *
 * MapLibre (as the plan chose) over OpenStreetMap raster tiles — the same
 * source StaticMap uses. The library is ~250 KB, so it is imported only
 * when a zone editor first opens: an author who never edits a place, and
 * every player, never downloads it.
 */
type MapLib = typeof import("maplibre-gl");
let loading: Promise<MapLib> | null = null;
export function loadMapLibre(): Promise<MapLib> {
  // MapLibre looks for its worker beside its own module, which bundling
  // moves; point it at the worker Vite builds instead.
  loading ??= Promise.all([import("maplibre-gl"), import("maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url"), import("maplibre-gl/dist/maplibre-gl.css")]).then(
    ([m, worker]) => {
      m.setWorkerUrl(new URL(worker.default, location.href).href);
      return m;
    },
    (e: unknown) => {
      // Forget the failure, so the next map to open tries again.
      loading = null;
      throw e;
    },
  );
  return loading;
}

// The operator's tile source (ZV_MAP_TILE_URL), set on the page as
// window.ZV_MAP by /assets/map-config.js; OpenStreetMap without it.
const mapConfig = (globalThis as { ZV_MAP?: { tile_url?: string; attribution?: string } }).ZV_MAP;

export const STYLE = {
  version: 8 as const,
  sources: {
    osm: {
      type: "raster" as const,
      tiles: [mapConfig?.tile_url || "https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
      tileSize: 256,
      maxzoom: 19,
      attribution: mapConfig?.attribution || "© OpenStreetMap contributors",
    },
  },
  layers: [{ id: "osm", type: "raster" as const, source: "osm" }],
};

function circleRing(z: Zone): [number, number][] {
  const dLat = z.radius_m / 111320;
  const dLng = z.radius_m / (111320 * Math.cos((z.lat * Math.PI) / 180));
  const ring: [number, number][] = [];
  for (let i = 0; i <= 64; i++) {
    const a = (i / 64) * 2 * Math.PI;
    ring.push([z.lng + dLng * Math.cos(a), z.lat + dLat * Math.sin(a)]);
  }
  return ring;
}

/** The zone as GeoJSON, in MapLibre's [lng, lat] order. */
export function shape(z: Zone) {
  const poly = z.polygon ?? [];
  const ring: [number, number][] =
    poly.length >= 3 ? [...poly.map(([la, ln]) => [ln, la] as [number, number]), [poly[0]![1], poly[0]![0]]] : poly.length ? poly.map(([la, ln]) => [ln, la] as [number, number]) : circleRing(z);
  return {
    type: "Feature" as const,
    properties: {},
    geometry: poly.length > 0 && poly.length < 3 ? { type: "LineString" as const, coordinates: ring } : { type: "Polygon" as const, coordinates: [ring] },
  };
}

/** A polygon's centre, for the lat/lng maps centre it on. */
function centroid(p: [number, number][]): [number, number] {
  const n = p.length || 1;
  return [p.reduce((s, q) => s + q[0], 0) / n, p.reduce((s, q) => s + q[1], 0) / n];
}

export function ZoneMap({ zone, onChange, areaOnly, maxCorners }: { zone: Zone; onChange: (z: Zone) => void; areaOnly?: boolean; maxCorners?: number }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<import("maplibre-gl").Map | null>(null);
  const libRef = useRef<MapLib | null>(null);
  const markers = useRef<import("maplibre-gl").Marker[]>([]);
  const zoneRef = useRef(zone);
  zoneRef.current = zone;
  const changeRef = useRef(onChange);
  changeRef.current = onChange;
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [finding, setFinding] = useState(false);
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);
  // Local, not derived: an area with no corners yet is still being drawn.
  const [polygonMode, setPolygonMode] = useState(areaOnly || (zone.polygon?.length ?? 0) > 0);
  const maxRef = useRef(maxCorners);
  maxRef.current = maxCorners;
  const modeRef = useRef(polygonMode);
  modeRef.current = polygonMode;

  // Create the map once.
  useEffect(() => {
    let cancelled = false;
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !box.current) return;
        libRef.current = lib;
        // On a touch screen one finger scrolls the page past the map and two
        // move the map, so the map inside a scrolling panel is not a trap.
        const map = new lib.Map({ container: box.current, style: STYLE, center: [zone.lng, zone.lat], zoom: 17, attributionControl: { compact: true }, cooperativeGestures: isTouch() });
        map.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        map.on("load", () => {
          map.addSource("zone", { type: "geojson", data: shape(zoneRef.current) });
          map.addLayer({ id: "zone-fill", type: "fill", source: "zone", paint: { "fill-color": conceptColor("place", "#1b6b43"), "fill-opacity": 0.18 } });
          map.addLayer({ id: "zone-line", type: "line", source: "zone", paint: { "line-color": conceptColor("place", "#1b6b43"), "line-width": 2 } });
          setReady(true);
        });
        // In polygon mode, a click adds the next corner.
        map.on("click", (e) => {
          if (!modeRef.current) return;
          const z = zoneRef.current;
          if (maxRef.current && (z.polygon?.length ?? 0) >= maxRef.current) return;
          const polygon = [...(z.polygon ?? []), [e.lngLat.lat, e.lngLat.lng] as [number, number]];
          const [lat, lng] = centroid(polygon);
          changeRef.current({ ...z, polygon, lat, lng });
        });
        mapRef.current = map;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      mapRef.current?.remove();
      mapRef.current = null;
    };
    // Created once; later changes flow in through the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Redraw the shape and its handles whenever the zone changes.
  useEffect(() => {
    const map = mapRef.current;
    const lib = libRef.current;
    if (!ready || !map || !lib) return;
    (map.getSource("zone") as import("maplibre-gl").GeoJSONSource | undefined)?.setData(shape(zone));
    for (const m of markers.current) m.remove();
    markers.current = [];
    const handle = (lat: number, lng: number, onDrag: (lat: number, lng: number) => void, className: string) => {
      const el = document.createElement("div");
      el.className = className;
      const m = new lib.Marker({ element: el, draggable: true }).setLngLat([lng, lat]).addTo(map);
      m.on("dragend", () => {
        const p = m.getLngLat();
        onDrag(p.lat, p.lng);
      });
      markers.current.push(m);
    };
    if (polygonMode) {
      (zone.polygon ?? []).forEach(([la, ln], i) =>
        handle(la, ln, (lat, lng) => {
          const polygon = (zoneRef.current.polygon ?? []).map((q, j) => (j === i ? ([lat, lng] as [number, number]) : q));
          const [clat, clng] = centroid(polygon);
          changeRef.current({ ...zoneRef.current, polygon, lat: clat, lng: clng });
        }, "zone-map__corner"),
      );
    } else {
      handle(zone.lat, zone.lng, (lat, lng) => changeRef.current({ ...zoneRef.current, lat, lng }), "zone-map__centre");
    }
  }, [zone, ready, polygonMode]);

  const setMode = (polygon: boolean) => {
    if (polygon === polygonMode) return;
    setPolygonMode(polygon);
    if (!polygon && zone.polygon) {
      const next = { ...zone };
      delete next.polygon;
      onChange(next);
    }
  };

  const corners = zone.polygon?.length ?? 0;

  // Move the pin, or add the next corner, where the phone is: an author
  // walking the ground marks it as they go.
  const here = async () => {
    setFinding(true);
    setNote(null);
    try {
      const p = await locate(t);
      const z = zoneRef.current;
      if (polygonMode) {
        const polygon = [...(z.polygon ?? []), [p.lat, p.lng] as [number, number]];
        const [lat, lng] = centroid(polygon);
        changeRef.current({ ...z, polygon, lat, lng });
      } else changeRef.current({ ...z, lat: p.lat, lng: p.lng });
      mapRef.current?.flyTo({ center: [p.lng, p.lat], zoom: Math.max(mapRef.current.getZoom(), 17) });
      setNote(p.accuracy > 50 ? { text: t.t("GPS is rough here (about {{m}} m). Wait a moment and try again.", { m: p.accuracy }), warn: true } : { text: t.t("Placed to within about {{m}} m.", { m: p.accuracy }) });
    } catch (e) {
      setNote({ text: e instanceof Error ? e.message : String(e), warn: true });
    } finally {
      setFinding(false);
    }
  };
  const hereButton = (
    <button type="button" className="link-btn zone-map__here" disabled={finding || (polygonMode && !!maxCorners && corners >= maxCorners)} onClick={() => void here()}>
      <LocateFixed size={ICON.sm} aria-hidden="true" />
      {finding ? t.t("Finding you…") : polygonMode ? t.t("Add a corner where I stand") : t.t("Use my location")}
    </button>
  );

  return (
    <div className="zone-map">
      {!areaOnly && (
        <div className="seg zone-map__mode" role="group" aria-label={t.t("Shape")}>
          <button type="button" className={!polygonMode ? "is-on" : ""} onClick={() => setMode(false)}>
            {t.t("Circle")}
          </button>
          <button type="button" className={polygonMode ? "is-on" : ""} onClick={() => setMode(true)}>
            {t.t("Area")}
          </button>
        </div>
      )}
      {failed ? (
        <p className="field__hint">{t.t("The map could not load. Check the connection.")}</p>
      ) : (
        // MapLibre owns the inner element's classes; React re-rendering them would drop its own.
        <div className={"zone-map__map" + (polygonMode ? " is-drawing" : "")}>
          <div ref={box} className="zone-map__gl" />
        </div>
      )}
      {polygonMode ? (
        <div className="zone-map__row">
          <span className="field__hint">
            {maxCorners === 4
              ? [t.t("Click the top-left corner."), t.t("Now the top-right corner."), t.t("Now the bottom-right corner."), t.t("Now the bottom-left corner.")][corners] ??
                t.t("Drag a corner to adjust the area.")
              : corners < 3
                ? t.t("Click the map to place corners — at least three.")
                : t.t("{{n}} corners. Drag a corner to move it; click to add more.", { n: corners })}
          </span>
          {hereButton}
          <button type="button" className="link-btn" disabled={!corners} onClick={() => onChange({ ...zone, polygon: (zone.polygon ?? []).slice(0, -1) })}>
            {t.t("Undo corner")}
          </button>
          <button type="button" className="link-btn" disabled={!corners} onClick={() => onChange({ ...zone, polygon: [] })}>
            {t.t("Clear")}
          </button>
        </div>
      ) : (
        <div className="zone-map__row">
          <span className="field__hint">{t.t("Drag the pin to the centre.")}</span>
          {hereButton}
          <label className="field field--num">
            <span className="field__label">{t.t("Radius m")}</span>
            <input className="field__input" type="number" min={1} value={zone.radius_m} onChange={(e) => onChange({ ...zone, radius_m: Number(e.target.value) || 0 })} />
          </label>
        </div>
      )}
      {note && <span className={"field__hint" + (note.warn ? " zone-here__warn" : "")}>{note.text}</span>}
    </div>
  );
}
