import { useCallback, useEffect, useMemo, useRef, useState } from "react";

/** A frame /ws/arrange pushes for a watched event (internal/admin/ws.go). */
export interface WatchMessage {
  kind: "event_detail" | "tracks" | "activity" | "watch_error" | string;
  data?: unknown;
}

/**
 * Follow an event live. /ws/arrange pushes the event's detail, tracks and
 * activity whenever anything in it changes; the page takes the payloads
 * it shows, or simply re-reads. One socket per page, reconnecting with
 * jittered backoff, released when the page goes.
 */
/** "live" once the watch is open; "reconnecting" after it dropped, until it is back. */
export type WatchState = "connecting" | "live" | "reconnecting";

export function useEventWatch(eventId: number | null, onMessage: (m: WatchMessage) => void): WatchState {
  return useWatch(eventId ? JSON.stringify({ kind: "watch_event", data: { event_id: eventId } }) : null, onMessage).state;
}

/** A watch that also talks back: presence, focus, pointers. */
export interface Live {
  state: WatchState;
  /** Sends a frame while the watch is open; dropped while it is not. */
  send: (kind: string, data: unknown) => void;
}

/**
 * Follow an adventure's draft: /ws/arrange pushes graph_saved whenever
 * it is saved — by an assistant, another tab, anyone.
 */
export function useAdventureWatch(slug: string | null, onMessage: (m: WatchMessage) => void): Live {
  return useWatch(slug ? JSON.stringify({ kind: "watch_adventure", data: { slug } }) : null, onMessage);
}

/** One /ws/arrange socket that sends `watch` on every (re)connect. */
function useWatch(watch: string | null, onMessage: (m: WatchMessage) => void): Live {
  const cb = useRef(onMessage);
  cb.current = onMessage;
  const [state, setState] = useState<WatchState>("connecting");
  const socket = useRef<WebSocket | null>(null);
  const send = useCallback((kind: string, data: unknown) => {
    const ws = socket.current;
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ kind, data }));
  }, []);
  useEffect(() => {
    if (!watch) return;
    let ws: WebSocket | null = null;
    let closed = false;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const receive = (ev: MessageEvent) => {
      try {
        const m = JSON.parse(String(ev.data)) as WatchMessage;
        if (m && typeof m.kind === "string") cb.current(m);
      } catch {
        /* not a frame of ours */
      }
    };
    const connect = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/ws/arrange`);
      socket.current = ws;
      ws.onopen = () => {
        retry = 1000;
        setState("live");
        ws?.send(watch);
      };
      ws.onmessage = receive;
      ws.onclose = () => {
        if (closed) return;
        setState("reconnecting");
        timer = setTimeout(connect, retry / 2 + Math.random() * retry);
        retry = Math.min(retry * 2, 30_000);
      };
    };
    connect();
    return () => {
      closed = true;
      clearTimeout(timer);
      ws?.close();
      socket.current = null;
    };
  }, [watch]);
  return useMemo(() => ({ state, send }), [state, send]);
}
