import { useEffect, useRef, useState } from "react";
import { useT } from "../../i18n.tsx";
import { loadMapLibre, mapLocale, shape, STYLE } from "../../designer/ZoneMap.tsx";
import type { Pin } from "./facts.ts";

type Map = import("maplibre-gl").Map;

/**
 * The adventure on a map: numbered pins (filled when the step is gated),
 * zones drawn as their shapes. `faint` draws unlabelled dots instead, for
 * the catalog — the shape of the adventure without its exact gates.
 */
export function AdventureMap({ pins, accent, faint, label, height = 300 }: { pins: Pin[]; accent?: string | undefined; faint?: boolean; label: string; height?: number }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const map = useRef<Map | null>(null);
  const [failed, setFailed] = useState(false);
  const color = accent || "#ad1357";

  useEffect(() => {
    let cancelled = false;
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !box.current) return;
        const first = pins[0];
        const m = new lib.Map({ container: box.current, style: STYLE, center: first ? [first.lng, first.lat] : [13.9554, 58.182], zoom: 13, attributionControl: { compact: true }, locale: mapLocale(t), cooperativeGestures: true });
        m.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        m.on("load", () => {
          // A pin's zone, or the circle of its radius where it has no zone:
          // the start, which teams must be within to begin.
          const zones = [...new Set(pins.flatMap((p) => (p.zone ? [p.zone] : p.radius_m ? [{ slug: "", lat: p.lat, lng: p.lng, radius_m: p.radius_m }] : [])))];
          if (zones.length && !faint) {
            m.addSource("zones", { type: "geojson", data: { type: "FeatureCollection", features: zones.map((z) => shape(z)) } });
            m.addLayer({ id: "zones-fill", type: "fill", source: "zones", paint: { "fill-color": color, "fill-opacity": 0.12 } });
            m.addLayer({ id: "zones-line", type: "line", source: "zones", paint: { "line-color": color, "line-width": 1.5 } });
          }
          if (faint) {
            m.addSource("dots", { type: "geojson", data: { type: "FeatureCollection", features: pins.map((p) => ({ type: "Feature" as const, properties: {}, geometry: { type: "Point" as const, coordinates: [p.lng, p.lat] } })) } });
            m.addLayer({ id: "dots", type: "circle", source: "dots", paint: { "circle-radius": 6, "circle-color": color, "circle-opacity": 0.7 } });
          }
        });
        if (!faint) {
          // One marker per place; steps sharing a zone share its pin.
          const seen = new Set<string>();
          pins.forEach((p, i) => {
            const key = `${p.lat},${p.lng}`;
            if (seen.has(key)) return;
            seen.add(key);
            const el = document.createElement("span");
            el.className = "adv-pin" + (p.gated ? " is-gated" : "");
            el.style.setProperty("--pin", color);
            el.textContent = String(i + 1);
            el.title = `${i + 1}. ${p.title}`;
            el.setAttribute("aria-label", el.title);
            new lib.Marker({ element: el }).setLngLat([p.lng, p.lat]).addTo(m);
          });
        }
        if (pins.length > 1 || (first?.radius_m && !faint)) {
          const b = new lib.LngLatBounds();
          for (const p of pins) {
            b.extend([p.lng, p.lat]);
            // Show the whole circle, not just its centre.
            if (p.radius_m && !faint) {
              const dLat = p.radius_m / 111320;
              const dLng = p.radius_m / (111320 * Math.cos((p.lat * Math.PI) / 180));
              b.extend([p.lng - dLng, p.lat - dLat]);
              b.extend([p.lng + dLng, p.lat + dLat]);
            }
          }
          m.fitBounds(b, { padding: 32, maxZoom: 16, animate: false });
        } else if (first) {
          m.setZoom(16);
        }
        map.current = m;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      map.current?.remove();
      map.current = null;
    };
    // The pins are the page's data, fixed while it is shown.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (failed) return <p className="cs-field__hint">{t.t("Map library failed to load — check your connection.")}</p>;
  return <div ref={box} className="adv-map" style={{ height }} role="region" aria-label={label} />;
}
