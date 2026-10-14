import { useEffect, useRef, useState } from "react";
import type { Zone } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { mapStyle, loadMapLibre, mapLocale, shape } from "./ZoneMap.tsx";
import { conceptColor } from "./dz/concepts.tsx";
import { defaultCentre } from "./zones.ts";

export interface Point {
  lat: number;
  lng: number;
}

/**
 * Where the simulated team stands, on a map: click to put it somewhere,
 * or drag its pin. The adventure's zones are drawn, so an author can
 * step just inside an edge, or just outside an "only if you are not
 * here" answer, without walking there.
 */
export function SpoofMap({ zones, position, focus, onPick, disabled }: { zones: Zone[]; position: Point | null; focus?: Zone | null | undefined; onPick: (p: Point) => void; disabled?: boolean }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<import("maplibre-gl").Map | null>(null);
  const pin = useRef<import("maplibre-gl").Marker | null>(null);
  const pickRef = useRef(onPick);
  pickRef.current = onPick;
  const disabledRef = useRef(disabled);
  disabledRef.current = disabled;
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !box.current) return;
        const first = position ?? zones[0] ?? defaultCentre();
        const map = new lib.Map({ container: box.current, style: mapStyle(), center: [first.lng, first.lat], zoom: 15, attributionControl: { compact: true }, locale: mapLocale(t) });
        map.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        map.on("load", () => {
          map.addSource("zones", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addLayer({ id: "zones-fill", type: "fill", source: "zones", paint: { "fill-color": conceptColor("place", "#1b6b43"), "fill-opacity": 0.15 } });
          map.addLayer({ id: "zones-line", type: "line", source: "zones", paint: { "line-color": conceptColor("place", "#1b6b43"), "line-width": 1.5 } });
          setReady(true);
        });
        map.on("click", (e) => {
          if (!disabledRef.current) pickRef.current({ lat: e.lngLat.lat, lng: e.lngLat.lng });
        });
        const el = document.createElement("div");
        el.className = "spoof-map__team";
        pin.current = new lib.Marker({ element: el, draggable: true });
        pin.current.on("dragend", () => {
          const p = pin.current!.getLngLat();
          pickRef.current({ lat: p.lat, lng: p.lng });
        });
        mapRef.current = map;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      mapRef.current?.remove();
      mapRef.current = null;
    };
    // Created once; zones and the pin follow through the effects below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const src = mapRef.current?.getSource("zones") as import("maplibre-gl").GeoJSONSource | undefined;
    src?.setData({ type: "FeatureCollection", features: zones.map(shape) });
  }, [zones, ready]);

  // Zoom to the place the step needs. A 10 m zone is a few pixels at
  // street level; framed, it is easy to click inside or just outside.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !focus) return;
    const g = shape(focus).geometry;
    const pts = (g.type === "Polygon" ? g.coordinates[0] : g.coordinates) ?? [];
    if (pts.length === 0) return;
    const lngs = pts.map((p) => p[0]);
    const lats = pts.map((p) => p[1]);
    map.fitBounds(
      [
        [Math.min(...lngs), Math.min(...lats)],
        [Math.max(...lngs), Math.max(...lats)],
      ],
      { padding: 40, maxZoom: 19, duration: 400 },
    );
    // Keyed on the zone, not the object: a new view of the same step
    // must not pull the map back while the author is looking around.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focus?.slug, ready]);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !pin.current) return;
    if (!position) {
      pin.current.remove();
      return;
    }
    pin.current.setLngLat([position.lng, position.lat]).addTo(map);
    // Keep the team in view when a zone is picked from the list.
    if (!map.getBounds().contains([position.lng, position.lat])) map.easeTo({ center: [position.lng, position.lat] });
  }, [position, ready]);

  if (failed) return <p className="field__hint">{t.t("The map could not load. Check the connection.")}</p>;
  return (
    <div className="spoof-map">
      <div className="spoof-map__gl" ref={box} />
      <span className="field__hint">{t.t("Click the map or drag the pin to move the team.")}</span>
    </div>
  );
}
