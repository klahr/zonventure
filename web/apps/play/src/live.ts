import { useEffect, useRef, useState } from "react";
import { play } from "@zonventure/api";

/**
 * Refetch whenever the server says something changed.
 *
 * /ws/play pushes on every change in the event — a teammate answering,
 * the organiser pausing. The push body is the lane player's state shape,
 * so this treats any message as "look again" and re-reads the graph view
 * over HTTP; one extra request per change is cheap next to keeping two
 * push formats in step.
 *
 * Reconnects with backoff. A slow poll stays on underneath, because a
 * socket that looks open on a phone moving between cells can be dead.
 */
export function useLiveRefresh(enabled: boolean, refresh: () => void) {
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    if (!enabled) return;
    let ws: WebSocket | null = null;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let debounce: ReturnType<typeof setTimeout> | undefined;
    let closed = false;

    const poke = () => {
      clearTimeout(debounce);
      debounce = setTimeout(() => refreshRef.current(), 250);
    };

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/ws/play`);
      ws.onopen = () => {
        retry = 1000;
        poke();
      };
      ws.onmessage = poke;
      ws.onclose = () => {
        if (closed) return;
        timer = setTimeout(connect, retry);
        retry = Math.min(retry * 2, 30_000);
      };
    };
    connect();
    const poll = setInterval(() => refreshRef.current(), 30_000);

    return () => {
      closed = true;
      clearTimeout(timer);
      clearTimeout(debounce);
      clearInterval(poll);
      ws?.close();
    };
  }, [enabled]);
}

export interface Geo {
  pos: GeolocationPosition | null;
  error: GeolocationPositionError | null;
}

/**
 * Watch the device's position and report it to the server.
 *
 * Reported every 15 seconds for the organiser's live map, and every 3
 * while the current step waits for the team to reach a zone, so the
 * "you are here" moment is quick. After a report during that wait the
 * view is re-read: whether the team is inside is the server's decision,
 * made with its anti-teleport rule, never the phone's.
 */
export function useGeo(enabled: boolean, waitingForZone: boolean, refresh: () => void): Geo {
  const [geo, setGeo] = useState<Geo>({ pos: null, error: null });
  const last = useRef(0);
  const waitingRef = useRef(waitingForZone);
  waitingRef.current = waitingForZone;
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    if (!enabled || !("geolocation" in navigator)) return;
    const id = navigator.geolocation.watchPosition(
      (pos) => {
        setGeo({ pos, error: null });
        const gap = waitingRef.current ? 3_000 : 15_000;
        const now = Date.now();
        if (now - last.current < gap) return;
        last.current = now;
        const c = pos.coords;
        void play
          .postLocation({
            lat: c.latitude,
            lng: c.longitude,
            accuracy: c.accuracy,
            ...(c.speed != null ? { speed: c.speed } : {}),
            ...(c.heading != null ? { heading: c.heading } : {}),
            client_ts: new Date(pos.timestamp).toISOString(),
          })
          .then(() => {
            if (waitingRef.current) refreshRef.current();
          })
          .catch(() => {
            /* offline: the next fix tries again */
          });
      },
      (error) => setGeo((g) => ({ ...g, error })),
      { enableHighAccuracy: true, maximumAge: 5_000, timeout: 30_000 },
    );
    return () => navigator.geolocation.clearWatch(id);
  }, [enabled]);

  return geo;
}

/** Great-circle distance in metres, for "about 120 m away". */
export function metresBetween(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const r = 6371000;
  const rad = (d: number) => (d * Math.PI) / 180;
  const dLat = rad(lat2 - lat1);
  const dLng = rad(lng2 - lng1);
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(rad(lat1)) * Math.cos(rad(lat2)) * Math.sin(dLng / 2) ** 2;
  return r * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}
