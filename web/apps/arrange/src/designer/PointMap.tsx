import { useEffect, useRef, useState } from "react";
import { LocateFixed } from "lucide-react";
import { useT } from "../i18n.tsx";
import { ICON } from "./dz/ui.tsx";
import { isTouch } from "./device.ts";
import { STYLE, circleRing, collapseCredits, loadMapLibre, mapLocale } from "./ZoneMap.tsx";
import { conceptColor } from "./dz/concepts.tsx";
import { DEFAULT_CENTRE, locate } from "./zones.ts";
import "./pointmap.css";

type Point = { lat: number; lng: number };

/** Six decimals is 10 cm: plenty for a pin, and a stable diff. */
const round = (n: number) => Math.round(n * 1e6) / 1e6;

/**
 * Pick one point on a map: click or tap to put the pin there, drag it,
 * or use where the phone is. For a place that is only a point, such as
 * where teams meet; a zone with a size is edited in ZoneMap. `radius`
 * only shows a circle around the pin.
 */
export function PointMap({ value, onChange, fallback, radius }: { value: Point | null; onChange: (p: Point) => void; fallback?: Point | null; radius?: number | null }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<import("maplibre-gl").Map | null>(null);
  const markerRef = useRef<import("maplibre-gl").Marker | null>(null);
  const libRef = useRef<Awaited<ReturnType<typeof loadMapLibre>> | null>(null);
  const changeRef = useRef(onChange);
  changeRef.current = onChange;
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [finding, setFinding] = useState(false);
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);

  useEffect(() => {
    let cancelled = false;
    const start = value ?? fallback ?? DEFAULT_CENTRE;
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !box.current) return;
        libRef.current = lib;
        const map = new lib.Map({ container: box.current, style: STYLE, center: [start.lng, start.lat], zoom: value ? 16 : 13, attributionControl: { compact: true }, locale: mapLocale(t), cooperativeGestures: isTouch() });
        map.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        collapseCredits(map);
        map.on("click", (e) => changeRef.current({ lat: round(e.lngLat.lat), lng: round(e.lngLat.lng) }));
        map.on("load", () => {
          map.addSource("radius", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addLayer({ id: "radius-fill", type: "fill", source: "radius", paint: { "fill-color": conceptColor("place", "#1b6b43"), "fill-opacity": 0.18 } });
          map.addLayer({ id: "radius-line", type: "line", source: "radius", paint: { "line-color": conceptColor("place", "#1b6b43"), "line-width": 2 } });
          setReady(true);
        });
        mapRef.current = map;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      mapRef.current?.remove();
      mapRef.current = null;
    };
    // Created once; the pin follows `value` below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // With no pin yet, the map shows the fallback, which may arrive after it opened.
  useEffect(() => {
    if (!ready || value || !fallback) return;
    mapRef.current?.jumpTo({ center: [fallback.lng, fallback.lat], zoom: 15 });
    // Only when the fallback itself changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, fallback?.lat, fallback?.lng]);

  // The pin sits on `value`, and is dragged to change it.
  useEffect(() => {
    const map = mapRef.current;
    const lib = libRef.current;
    if (!ready || !map || !lib) return;
    if (!value) {
      markerRef.current?.remove();
      markerRef.current = null;
      return;
    }
    if (!markerRef.current) {
      const el = document.createElement("div");
      el.className = "point-map__pin";
      const m = new lib.Marker({ element: el, draggable: true }).setLngLat([value.lng, value.lat]).addTo(map);
      m.on("dragend", () => {
        const p = m.getLngLat();
        changeRef.current({ lat: round(p.lat), lng: round(p.lng) });
      });
      markerRef.current = m;
    } else markerRef.current.setLngLat([value.lng, value.lat]);
  }, [value, ready]);

  // The circle follows the pin and the radius.
  useEffect(() => {
    if (!ready) return;
    const ring = value && radius && radius > 0 ? circleRing({ lat: value.lat, lng: value.lng, radius_m: radius }) : null;
    (mapRef.current?.getSource("radius") as import("maplibre-gl").GeoJSONSource | undefined)?.setData(
      ring ? { type: "Feature", properties: {}, geometry: { type: "Polygon", coordinates: [ring] } } : { type: "FeatureCollection", features: [] },
    );
  }, [value?.lat, value?.lng, radius, ready]);

  const here = async () => {
    setFinding(true);
    setNote(null);
    try {
      const p = await locate(t);
      changeRef.current({ lat: round(p.lat), lng: round(p.lng) });
      mapRef.current?.flyTo({ center: [p.lng, p.lat], zoom: Math.max(mapRef.current.getZoom(), 16) });
      setNote(p.accuracy > 50 ? { text: t.t("GPS is rough here (about {{m}} m). Wait a moment and try again.", { m: p.accuracy }), warn: true } : { text: t.t("Placed to within about {{m}} m.", { m: p.accuracy }) });
    } catch (e) {
      setNote({ text: e instanceof Error ? e.message : String(e), warn: true });
    } finally {
      setFinding(false);
    }
  };

  return (
    <div className="point-map">
      {failed ? (
        <p className="field__hint">{t.t("The map could not load. Check the connection.")}</p>
      ) : (
        // MapLibre owns the inner element's classes; React re-rendering them would drop its own.
        <div className="point-map__map">
          <div ref={box} className="point-map__gl" />
        </div>
      )}
      <div className="point-map__row">
        <span className="field__hint">{value ? t.t("Click the map to move the pin, or drag it.") : t.t("Click the map to put the pin there.")}</span>
        <button type="button" className="point-map__here" disabled={finding} onClick={() => void here()}>
          <LocateFixed size={ICON.sm} aria-hidden="true" />
          {finding ? t.t("Finding you…") : t.t("Use my location")}
        </button>
      </div>
      {note && <span className={"field__hint" + (note.warn ? " point-map__warn" : "")}>{note.text}</span>}
    </div>
  );
}
