import { useSyncExternalStore } from "react";
import type { I18n } from "@zonventure/i18n";
import { toDate } from "../../ui/index.ts";
import type { EventDetail, EventInfo, Team } from "./api.ts";

/** The classic console's team palette, so a team keeps its colour across both. */
const TEAM_COLORS = ["#e24080", "#429a3f", "#2563eb", "#f59e0b", "#7c3aed", "#0891b2", "#be185d", "#16a34a"];
export const teamColor = (i: number) => TEAM_COLORS[((i % TEAM_COLORS.length) + TEAM_COLORS.length) % TEAM_COLORS.length]!;

export const ZONE_COLOR = "#ad1357";

export const runLabel = (e: EventInfo, t: I18n) => e.name || t.t("Event #{{id}}", { id: e.id });

export const stepTitle = (d: EventDetail | null, slug: string | undefined) => d?.graph?.titles?.[slug ?? ""] || slug || "";

const fmtPoints = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

/** How far a team has got: points and the step it is on. */
export function teamProgress(d: EventDetail, tm: Team, t: I18n): { main: string; step: string } {
  const done = !!tm.completed_at || !!tm.outcome;
  return { main: t.t("{{n}} points", { n: fmtPoints(tm.score ?? 0) }), step: done ? "" : stepTitle(d, tm.node) };
}

/**
 * Seconds the event has run, on the server's clock (offset corrects a
 * device clock that is off), frozen once it ends or pauses, and leaving
 * out earlier pauses.
 */
export function elapsedSeconds(e: EventInfo, offsetMs: number, now: number): number {
  const start = toDate(e.started_at)?.getTime();
  if (start === undefined) return 0;
  return Math.max(0, Math.floor((clockEnd(e, offsetMs, now) - start) / 1000 - (e.paused_seconds ?? 0)));
}

/** Where the event's clock stands, in server time: now, or when it was paused, ended or cancelled. */
function clockEnd(e: EventInfo, offsetMs: number, now: number): number {
  let end = now + offsetMs;
  for (const ts of [e.completed_at, e.cancelled_at, e.paused_at]) {
    const ms = toDate(ts)?.getTime();
    if (ms !== undefined && ms < end) end = ms;
  }
  return end;
}

/**
 * How long a split team's group has been on its step, in play time. The
 * server moves since past the pauses that ended since the group got
 * there (internal/admin.groupsOf), so only a pause still on, the end and
 * the clock offset are left to take in here.
 */
export function groupSeconds(e: EventInfo, since: string | undefined, offsetMs: number, now: number): number | undefined {
  const at = toDate(since)?.getTime();
  if (at === undefined) return undefined;
  return Math.max(0, Math.floor((clockEnd(e, offsetMs, now) - at) / 1000));
}

export function fmtElapsedShort(sec: number): string {
  if (sec <= 0) return "0m";
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  if (h >= 24) return `${Math.floor(h / 24)}d ${h % 24}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

// One shared second tick, for every clock and "5m ago" on the page.
let now = Date.now();
const subs = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;
function subscribe(cb: () => void) {
  now = Date.now();
  subs.add(cb);
  timer ??= setInterval(() => {
    now = Date.now();
    for (const s of subs) s();
  }, 1000);
  return () => {
    subs.delete(cb);
    if (subs.size === 0) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}
export const useNow = () => useSyncExternalStore(subscribe, () => now);

/** Filter and map viewport per event, kept while the console is open, as the classic console does. */
export const filters = new Map<number, Set<number>>();
export const mapViews = new Map<number, { center: [number, number]; zoom: number }>();

/** Activity race control leaves out: setup history, which the event page keeps. */
export const QUIET_KINDS = new Set(["event.created", "team.created", "event.started", "event.paused", "event.resumed"]);
