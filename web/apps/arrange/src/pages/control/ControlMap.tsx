import { useEffect, useRef, useState } from "react";
import type { Map as GLMap, Marker, GeoJSONSource, LngLatBounds } from "maplibre-gl";
import { metresBetween, type Zone } from "@zonventure/graph";
import { useT } from "../../i18n.tsx";
import { loadMapLibre, mapLocale, shape, mapStyle } from "../../designer/ZoneMap.tsx";
import { fmtDateTime, initials, toDate } from "../../ui/index.ts";
import type { Track } from "./api.ts";
import { mapViews, ZONE_COLOR } from "./model.ts";

type MapLib = Awaited<ReturnType<typeof loadMapLibre>>;

export interface MapTeam {
  id: number;
  name: string;
  color: string;
}

/**
 * Race control's map: the adventure's zones, each team's trail and its
 * latest position.
 *
 * The map is made once per page; a push moves markers and swaps the
 * GeoJSON rather than rebuilding, so tiles, the viewport and an open
 * popup stay put. Until the operator pans or zooms it follows the teams.
 */
export function ControlMap({ eventId, zones, teams, tracks }: { eventId: number; zones: Zone[]; teams: MapTeam[]; tracks: Track[] }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<GLMap | null>(null);
  const libRef = useRef<MapLib | null>(null);
  const touched = useRef(false);
  // One marker per track: a team's, or each group's while it is split up.
  const teamMarkers = useRef(new Map<string, { marker: Marker; key: string }>());
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
          style: mapStyle(),
          center: saved ? saved.center : [13.9554, 58.182],
          zoom: saved ? saved.zoom : 14,
          attributionControl: { compact: true },
          locale: mapLocale(t),
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
    };
    // The map is made once per event; a language switch keeps it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId]);

  // The fixed features: zones.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    const features = zones.map((z) => ({ ...shape({ ...z, radius_m: z.radius_m || 20 }), properties: { name: z.name || z.slug } }));
    (map.getSource("zones") as GeoJSONSource | undefined)?.setData({ type: "FeatureCollection", features });
  }, [ready, zones]);

  // The teams: trails, and a marker where each was last seen.
  useEffect(() => {
    const map = mapRef.current, lib = libRef.current;
    if (!ready || !map || !lib) return;
    const byId = new Map(teams.map((tm) => [tm.id, tm]));
    const shown = tracks.filter((tr) => byId.has(tr.team_id) && (tr.points?.length ?? 0) > 0);
    // Where each team, or each group, is now; a past trail is drawn but not
    // marked. A team or group may come as several trails (one per stretch
    // it was out); only one is marked, the newest that is not past.
    const newest = new Map<string, Track>();
    for (const tr of shown) {
      if (tr.past) continue;
      const had = newest.get(trackId(tr));
      if (!had || lastSeen(tr) >= lastSeen(had)) newest.set(trackId(tr), tr);
    }
    const current = [...newest.values()];
    (map.getSource("tracks") as GeoJSONSource | undefined)?.setData({
      type: "FeatureCollection",
      features: shown.map((tr) => ({
        type: "Feature" as const,
        properties: { color: byId.get(tr.team_id)!.color },
        geometry: { type: "MultiLineString" as const, coordinates: trailRuns(tr.points!) },
      })),
    });
    const keep = new Set<string>();
    for (const tr of current) {
      const tm = byId.get(tr.team_id)!;
      const last = tr.points![tr.points!.length - 1]!;
      const id = trackId(tr);
      keep.add(id);
      const tag = groupTag(tr);
      const key = `${tm.name}|${tm.color}|${tr.avatar_url ?? ""}|${tr.group_label ?? ""}|${tag ?? ""}`;
      let entry = teamMarkers.current.get(id);
      if (entry && entry.key !== key) {
        entry.marker.remove();
        entry = undefined;
      }
      if (!entry) {
        entry = { marker: new lib.Marker({ element: teamMarker(tm, tr.avatar_url, tr.group_label, tag) }).setLngLat([last.lng, last.lat]).setPopup(new lib.Popup({ offset: 18 })).addTo(map), key };
        teamMarkers.current.set(id, entry);
      }
      entry.marker.setLngLat([last.lng, last.lat]);
      const pop = document.createElement("div");
      const name = document.createElement("strong");
      name.textContent = tr.group_label ? `${tm.name} · ${tr.group_label}` : tm.name;
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
      extendAll(b, zones);
      for (const tr of current) for (const p of tr.points!) b.extend([p.lng, p.lat]);
      if (!b.isEmpty()) {
        touched.current = false;
        map.fitBounds(b, { padding: 32, animate: false, maxZoom: 17 });
      }
    }
  }, [ready, teams, tracks, zones, eventId, t]);

  const anyPosition = tracks.some((tr) => (tr.points?.length ?? 0) > 0 && teams.some((tm) => tm.id === tr.team_id));
  return (
    <div className="cc-map">
      {failed ? (
        <p className="cs-muted cs-flush">{t.t("Map library failed to load — check your connection.")}</p>
      ) : (
        <div className="cc-map__frame">
          <div ref={box} className="cc-map__gl" role="region" aria-label={t.t("Map of team positions")} />
        </div>
      )}
      {!failed && !anyPosition && <p className="cs-muted cs-flush cc-map__note">{t.t("No team positions yet.")}</p>}
    </div>
  );
}

/**
 * A trail as the runs between GPS jumps: a step faster than 60 m/s
 * (judged over at least 5 s, as the server's anti-teleport rule is) is a
 * stray fix, not a walk, so no line is drawn across it.
 */
function trailRuns(points: NonNullable<Track["points"]>): [number, number][][] {
  const runs: [number, number][][] = [];
  let run: [number, number][] = [];
  points.forEach((p, i) => {
    const prev = points[i - 1];
    if (prev) {
      const secs = ((toDate(p.server_ts)?.getTime() ?? 0) - (toDate(prev.server_ts)?.getTime() ?? 0)) / 1000;
      if (metresBetween(prev.lat, prev.lng, p.lat, p.lng) > 60 * Math.max(5, secs)) {
        runs.push(run);
        run = [];
      }
    }
    run.push([p.lng, p.lat]);
  });
  runs.push(run);
  return runs.filter((r) => r.length > 1);
}

/** When a track's last point was taken, for comparing (the server's timestamps sort as text). */
const lastSeen = (tr: Track): string => tr.points?.[tr.points.length - 1]?.server_ts ?? "";

/** A track's own key: its team's, and its group's while the team is split up. */
const trackId = (tr: Track) => (tr.group ? `${tr.team_id}/${tr.group}` : String(tr.team_id));

/** Which group of the team a track is: the innermost path's name. A label's own " › " is only split for a server that sends no parts. */
const groupTag = (tr: Track): string | undefined =>
  tr.group_labels?.length ? tr.group_labels[tr.group_labels.length - 1] : tr.group_label ? (tr.group_label.split(" › ").pop() ?? tr.group_label) : undefined;

function teamMarker(tm: MapTeam, avatar: string | undefined, group?: string, tag?: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "cc-team-marker";
  el.style.setProperty("--team-col", tm.color);
  el.title = group ? `${tm.name} · ${group}` : tm.name;
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
  if (group) {
    el.classList.add("cc-team-marker--group");
    const small = document.createElement("small");
    small.className = "cc-team-marker__group";
    small.textContent = tag ?? group;
    el.append(small);
  }
  return el;
}

function extendAll(b: LngLatBounds, zones: Zone[]) {
  for (const z of zones) {
    const g = shape({ ...z, radius_m: z.radius_m || 20 }).geometry;
    const ring = g.type === "Polygon" ? g.coordinates[0]! : g.coordinates;
    for (const c of ring) b.extend(c);
  }
}
