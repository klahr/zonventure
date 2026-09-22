import { useEffect, useMemo, useState } from "react";

/**
 * The engine's current time, in milliseconds.
 *
 * Every deadline in a View is relative to view.now, the engine's clock —
 * never the phone's, which may be minutes out. Live, the clock runs on
 * from view.now at the device's rate, so a countdown stays right even on
 * a phone set to the wrong time. Frozen, it stays at view.now, which is
 * what the simulator wants: its clock moves only when the author says.
 */
export function useEngineClock(nowIso: string, live: boolean): number {
  const base = Date.parse(nowIso);
  // When this view arrived. Keyed on nowIso so every new view restarts
  // the count from its own timestamp.
  const receivedAt = useMemo(() => Date.now(), [nowIso]);
  const [, tick] = useState(0);

  useEffect(() => {
    if (!live) return;
    const id = setInterval(() => tick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [live]);

  return live ? base + (Date.now() - receivedAt) : base;
}

/** 83 → "1:23", 3723 → "1:02:03". Negative clamps to zero. */
export function formatSeconds(total: number): string {
  const s = Math.max(0, Math.ceil(total));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

/** Seconds from `now` until an ISO time; negative once it has passed. */
export function secondsUntil(iso: string | undefined, now: number): number {
  return iso ? (Date.parse(iso) - now) / 1000 : 0;
}
