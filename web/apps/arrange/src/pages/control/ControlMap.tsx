import { useEffect, useRef, useState } from "react";
import type { Map as GLMap, Marker, GeoJSONSource, LngLatBounds } from "maplibre-gl";
import type { Zone } from "@zonventure/graph";
import { useT } from "../../i18n.tsx";
import { loadMapLibre, shape, STYLE } from "../../designer/ZoneMap.tsx";
import { fmtDateTime, initials } from "../../ui/index.ts";
import type { LanePage, Track } from "./api.ts";
import { mapViews, ZONE_COLOR } from "./model.ts";

type MapLib = Awaited<ReturnType<typeof loadMapLibre>>;

export interface MapTeam {
  id: number;
  name: string;
  color: string;
}

/**
 * Race control's map: the adventure's zones (a graph event) or puzzle
 * pins (a lane event), each team's trail and its latest position.
 *
 * The map is made once per page; a push moves markers and swaps the
 * GeoJSON rather than rebuilding, so tiles, the viewport and an open
 * popup stay put. Until the operator pans or zooms it follows the teams.
 */
export function ControlMap({ eventId, zones, pages, teams, tracks }: { eventId: number; zones: Zone[] | null; pages: LanePage[] | null; teams: MapTeam[]; tracks: Track[] }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<GLMap | null>(null);
  const libRef = useRef<MapLib | null>(null);
  const touched = useRef(false);
  const pins = useRef<Marker[]>([]);
  const teamMarkers = useRef(new Map<number, { marker: Marker; key: string }>());
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const el = box.current;
    const touch = () => { touched.current = true; };
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !el) return;
        libRef.current = lib;
        const saved = mapViews.get(eventId);
        const map = new lib.Map({
          container: el,
          style: STYLE,
          center: saved ? saved.center : [13.9554, 58.182],
          zoom: saved ? saved.zoom : 14,
          attributionControl: { compact: true },
          // Scrolling the page past the map shouldn't zoom it.
          cooperativeGestures: true,
        });
        map.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        map.addControl(new lib.FullscreenControl(), "top-right");
        // Only a move the operator started is kept: fitBounds moves the map too.
        el.addEventListener("pointerdown", touch);
        el.addEventListener("wheel", touch, { passive: true });
        el.addEventListener("keydown", touch);
        map.on("moveend", () => {
          if (!touched.current) return;
          const c = map.getCenter();
          mapViews.set(eventId, { center: [c.lng, c.lat], zoom: map.getZoom() });
        });
        map.on("load", () => {
          map.addSource("zones", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addSource("tracks", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addLayer({ id: "zones-fill", type: "fill", source: "zones", paint: { "fill-color": ZONE_COLOR, "fill-opacity": 0.12 } });
          map.addLayer({ id: "zones-line", type: "line", source: "zones", paint: { "line-color": ZONE_COLOR, "line-width": 2 } });
          map.addLayer({
            id: "tracks-line",
            type: "line",
            source: "tracks",
            layout: { "line-join": "round", "line-cap": "round" },
            paint: { "line-color": ["get", "color"], "line-width": 3, "line-opacity": 0.85 },
          });
          map.on("click", "zones-fill", (e) => {
            const name = e.features?.[0]?.properties?.name;
            if (!name) return;
            const strong = document.createElement("strong");
            strong.textContent = String(name);
            new lib.Popup().setLngLat(e.lngLat).setDOMContent(strong).addTo(map);
          });
          map.on("mouseenter", "zones-fill", () => { map.getCanvas().style.cursor = "pointer"; });
          map.on("mouseleave", "zones-fill", () => { map.getCanvas().style.cursor = ""; });
          setReady(true);
        });
        mapRef.current = map;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      el?.removeEventListener("pointerdown", touch);
      el?.removeEventListener("wheel", touch);
      el?.removeEventListener("keydown", touch);
      mapRef.current?.remove();
      mapRef.current = null;
      teamMarkers.current.clear();
      pins.current = [];
    };
  }, [eventId]);

  // The fixed features: zones, or puzzle pins.
  useEffect(() => {
    const map = mapRef.current, lib = libRef.current;
    if (!ready || !map || !lib) return;
    const features = (zones ?? []).map((z) => ({ ...shape({ ...z, radius_m: z.radius_m || 20 }), properties: { name: z.name || z.slug } }));
    (map.getSource("zones") as GeoJSONSource | undefined)?.setData({ type: "FeatureCollection", features });
    for (const m of pins.current) m.remove();
    pins.current = [];
    if (zones) return;
    for (const p of pages ?? []) {
      const el = document.createElement("div");
      el.className = "cc-pin" + (p.gated ? " is-gated" : "");
      const num = document.createElement("span");
      num.textContent = String(p.index + 1);
      el.append(num);
      const pop = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = `${p.index + 1}. ${p.title || t.t("Puzzle")}`;
      pop.append(title);
      if (p.gated && p.location.radius_m) {
        const g = document.createElement("div");
        g.textContent = t.t("Gated — {{r}} m", { r: p.location.radius_m });
        pop.append(g);
      }
      const ll = document.createElement("small");
      ll.textContent = `${p.location.lat.toFixed(5)}, ${p.location.lng.toFixed(5)}`;
      pop.append(ll);
      pins.current.push(
        new lib.Marker({ element: el, anchor: "bottom" })
          .setLngLat([p.location.lng, p.location.lat])
          .setPopup(new lib.Popup({ offset: 24 }).setDOMContent(pop))
          .addTo(map),
      );
    }
  }, [ready, zones, pages, t]);

  // The teams: trails, and a marker where each was last seen.
  useEffect(() => {
    const map = mapRef.current, lib = libRef.current;
    if (!ready || !map || !lib) return;
    const byId = new Map(teams.map((tm) => [tm.id, tm]));
    const shown = tracks.filter((tr) => byId.has(tr.team_id) && (tr.points?.length ?? 0) > 0);
    (map.getSource("tracks") as GeoJSONSource | undefined)?.setData({
      type: "FeatureCollection",
      features: shown.map((tr) => ({
        type: "Feature" as const,
        properties: { color: byId.get(tr.team_id)!.color },
        geometry: { type: "LineString" as const, coordinates: tr.points!.map((p) => [p.lng, p.lat]) },
      })),
    });
    const keep = new Set<number>();
    for (const tr of shown) {
      const tm = byId.get(tr.team_id)!;
      const last = tr.points![tr.points!.length - 1]!;
      keep.add(tm.id);
      const key = `${tm.name}|${tm.color}|${tr.avatar_url ?? ""}`;
      let entry = teamMarkers.current.get(tm.id);
      if (entry && entry.key !== key) {
        entry.marker.remove();
        entry = undefined;
      }
      if (!entry) {
        entry = { marker: new lib.Marker({ element: teamMarker(tm, tr.avatar_url) }).setLngLat([last.lng, last.lat]).setPopup(new lib.Popup({ offset: 18 })).addTo(map), key };
        teamMarkers.current.set(tm.id, entry);
      }
      entry.marker.setLngLat([last.lng, last.lat]);
      const pop = document.createElement("div");
      const name = document.createElement("strong");
      name.textContent = tm.name;
      const when = document.createElement("small");
      when.textContent = fmtDateTime(last.server_ts, t.current());
      pop.append(name, document.createElement("br"), when);
      entry.marker.getPopup()?.setDOMContent(pop);
    }
    for (const [id, entry] of teamMarkers.current) {
      if (!keep.has(id)) {
        entry.marker.remove();
        teamMarkers.current.delete(id);
      }
    }
    if (!mapViews.has(eventId)) {
      const b = new lib.LngLatBounds();
      extendAll(b, zones, pages);
      for (const tr of shown) for (const p of tr.points!) b.extend([p.lng, p.lat]);
      if (!b.isEmpty()) {
        touched.current = false;
        map.fitBounds(b, { padding: 32, animate: false, maxZoom: 17 });
      }
    }
  }, [ready, teams, tracks, zones, pages, eventId, t]);

  const anyPosition = tracks.some((tr) => (tr.points?.length ?? 0) > 0 && teams.some((tm) => tm.id === tr.team_id));
  return (
    <div className="cc-map">
      {failed ? (
        <p className="cc-muted">{t.t("Map library failed to load — check your connection.")}</p>
      ) : (
        <div className="cc-map__frame">
          <div ref={box} className="cc-map__gl" role="region" aria-label={t.t("Map of team positions")} />
        </div>
      )}
      {!failed && !anyPosition && <p className="cc-muted cc-map__note">{t.t("No team positions yet.")}</p>}
    </div>
  );
}

function teamMarker(tm: MapTeam, avatar: string | undefined): HTMLElement {
  const el = document.createElement("div");
  el.className = "cc-team-marker";
  el.style.setProperty("--team-col", tm.color);
  el.title = tm.name;
  const span = document.createElement("span");
  span.textContent = initials(tm.name);
  el.append(span);
  if (avatar) {
    // The initials stay underneath: a picture that fails to load is transparent.
    const img = document.createElement("img");
    img.src = avatar;
    img.alt = "";
    el.append(img);
  }
  return el;
}

function extendAll(b: LngLatBounds, zones: Zone[] | null, pages: LanePage[] | null) {
  for (const z of zones ?? []) {
    const g = shape({ ...z, radius_m: z.radius_m || 20 }).geometry;
    const ring = g.type === "Polygon" ? g.coordinates[0]! : g.coordinates;
    for (const c of ring) b.extend(c);
  }
  if (!zones) for (const p of pages ?? []) b.extend([p.location.lng, p.location.lat]);
}
