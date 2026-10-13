import { useEffect, useRef, useState } from "react";
import { ApiError, play } from "@zonventure/api";

/**
 * Refetch whenever the server says something changed.
 *
 * /ws/play pushes on every change in the event — a teammate answering,
 * the organiser pausing. Any message means "look again": the view is
 * re-read over HTTP.
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
        // Jittered, so every phone doesn't reconnect in the same second
        // after a deploy.
        timer = setTimeout(connect, retry / 2 + Math.random() * retry);
        retry = Math.min(retry * 2, 30_000);
      };
    };
    connect();
    // A hidden tab catches up when it comes back (see useGame), so the
    // poll only runs while the page is on screen.
    const poll = setInterval(() => {
      if (document.visibilityState === "visible") refreshRef.current();
    }, 30_000);

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
 * How often a phone that has not moved still reports while the step waits
 * on a zone. The browser only sends a fix when the position changes, and a
 * player standing still at their post must keep counting as there: the
 * server stops trusting a phone it has not heard from in a minute.
 */
const HEARTBEAT_MS = 15_000;

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
    let trailing: ReturnType<typeof setTimeout> | undefined;
    let latest: GeolocationPosition | undefined;
    let stopped = false;
    const report = (pos: GeolocationPosition, force: boolean) => {
      latest = pos;
      setGeo({ pos, error: null });
      const gap = waitingRef.current ? 3_000 : 15_000;
      const now = Date.now();
      // A fix a kilometre wide says nothing about where the team is.
      if (pos.coords.accuracy > 1_000) return;
      clearTimeout(trailing);
      if (!force && now - last.current < gap) {
        // Sent when the gap ends rather than dropped: the fix that
        // arrives at the place may be the last one a phone standing
        // still there gives.
        trailing = setTimeout(() => report(pos, true), gap - (now - last.current));
        return;
      }
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
        .catch((e: unknown) => {
          // Too soon after the last report: send this fix again once the
          // server allows, unless a newer one has come meanwhile. Any other
          // failure (offline) waits for the next fix.
          if (!(e instanceof ApiError) || e.status !== 429 || latest !== pos || stopped) return;
          clearTimeout(trailing);
          trailing = setTimeout(() => {
            if (latest === pos) report(pos, true);
          }, Math.min(Math.max(e.retryAfter ?? 2, 1), 30) * 1000);
        });
    };
    const onError = (error: GeolocationPositionError) => setGeo((g) => ({ ...g, error }));
    const id = navigator.geolocation.watchPosition((pos) => report(pos, false), onError, {
      enableHighAccuracy: true,
      maximumAge: 5_000,
      timeout: 30_000,
    });
    // Asks for a fix only when the watch has gone quiet during a wait, so
    // a walking phone is not asked twice.
    const heartbeat = setInterval(() => {
      if (!waitingRef.current || Date.now() - last.current < HEARTBEAT_MS) return;
      navigator.geolocation.getCurrentPosition((pos) => report(pos, true), onError, {
        enableHighAccuracy: true,
        maximumAge: 10_000,
        timeout: 10_000,
      });
    }, HEARTBEAT_MS / 3);
    return () => {
      stopped = true;
      navigator.geolocation.clearWatch(id);
      clearInterval(heartbeat);
      clearTimeout(trailing);
    };
  }, [enabled]);

  return geo;
}
