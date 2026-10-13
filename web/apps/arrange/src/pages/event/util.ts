import type { I18n } from "@zonventure/i18n";
import { toDate, type Tone } from "../../ui/index.ts";
import type { EventGraph, EventInfo, Team } from "./api.ts";

/**
 * canDo mirrors the server's eventstate matrix: the detail payload
 * carries allowed_ops, and the page hides or disables what the server
 * would refuse with a 409. A payload without the list allows all.
 */
export function canDo(event: EventInfo, op: string): boolean {
  return !Array.isArray(event.allowed_ops) || event.allowed_ops.includes(op);
}

export const isQuest = (event: EventInfo) => event.public_for_sale === true;
export const isLive = (event: EventInfo) => event.state === "running" || event.state === "paused";
export const isTerminal = (event: EventInfo) => event.state === "completed" || event.state === "cancelled";

export function runLabel(event: EventInfo): string {
  return event.name ? event.name : `Event #${event.id}`;
}

/**
 * Manual-start events read as "manual": a scheduled row with no time must
 * not promise one. A party adventure starts when its first team joins.
 */
export function stateLabel(event: EventInfo): string {
  if (event.state === "scheduled" && !event.scheduled_at) return event.party ? "waiting" : "manual";
  return event.state;
}

const STATE_TONE: Record<string, Tone> = { running: "good", scheduled: "accent", paused: "warn", completed: "good", cancelled: "bad" };
export const stateTone = (s: string): Tone => STATE_TONE[s] ?? "neutral";

// A non-colour cue for each state, as the classic console's pills have.
const GLYPH: Record<string, string> = { running: "▶", scheduled: "⏰", paused: "⏸", completed: "✓", cancelled: "✕" };
export const stateGlyph = (s: string) => (GLYPH[s] ? GLYPH[s] + " " : "");

export function stateText(t: I18n, s: string): string {
  switch (s) {
    case "manual": return t.t("not scheduled");
    case "waiting": return t.t("waiting for the first team");
    case "scheduled": return t.t("scheduled");
    case "running": return t.t("running");
    case "paused": return t.t("paused");
    case "completed": return t.t("completed");
    case "cancelled": return t.t("cancelled");
    default: return s;
  }
}

const TEAM_COLORS = ["#e24080", "#429a3f", "#2563eb", "#f59e0b", "#7c3aed", "#0891b2", "#be185d", "#16a34a"];
/** The team's colour by its place in the event, matching race control's leaderboard. */
export const teamColor = (i: number) => TEAM_COLORS[((i % TEAM_COLORS.length) + TEAM_COLORS.length) % TEAM_COLORS.length]!;

export const members = (tm: Team) => tm.members ?? [];
export const joinedCount = (tm: Team) => members(tm).filter((m) => m.last_login_at).length;

const STUCK_MS = 10 * 60 * 1000;

/**
 * A team is only "stuck" once someone has joined, and the quiet spell
 * counts from its latest sign of life — the event start or a player
 * joining count too — so a team created before the start, or one
 * nobody has opened yet, isn't flagged.
 */
export function teamStuck(event: EventInfo, tm: Team, now = Date.now()): boolean {
  if (event.state !== "running" || tm.completed_at || tm.withdrawn_at) return false;
  const logins = members(tm).map((m) => m.last_login_at).filter(Boolean);
  if (logins.length === 0) return false;
  const times = [tm.last_activity_at, event.started_at, ...logins]
    .map((x) => toDate(x)?.getTime())
    .filter((x): x is number => typeof x === "number");
  return times.length > 0 && now - Math.max(...times) > STUCK_MS;
}

export const fmtPoints = (n: number | undefined) => (Number.isInteger(n ?? 0) ? String(n ?? 0) : Number(n ?? 0).toFixed(1));

export const stepTitle = (graph: EventGraph | null | undefined, slug: string | undefined) => (slug ? graph?.titles?.[slug] || slug : "");

/**
 * How far a team has got: its points, the step it is on, and whether
 * it is done.
 */
export function teamProgress(t: I18n, graph: EventGraph | null | undefined, tm: Team) {
  const done = !!tm.completed_at || !!tm.outcome;
  return { main: t.t("{{n}} points", { n: fmtPoints(tm.score) }), step: done ? "" : stepTitle(graph, tm.node), done };
}

/** "1h 12m", "5m", "40s" — short enough for a stat tile. */
export function fmtElapsedShort(sec: number): string {
  if (sec <= 0) return "0m";
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

export const teamURL = (tm: Team) => `${location.origin}/team/${encodeURIComponent(tm.token || "")}`;
export const memberURL = (token: string) => `${location.origin}/play?t=${encodeURIComponent(token || "")}`;
export const recapURL = (token: string) => `${location.origin}/recap/${encodeURIComponent(token)}`;
export const liveURL = (event: EventInfo) => `${location.origin}/live/${encodeURIComponent(event.live_token || "")}`;

/** Write to the clipboard; false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function downloadBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** Shrink a photo to at most maxDim on its longer side before upload, as the classic console does. */
export async function downscaleImage(file: File, maxDim = 1600, quality = 0.85): Promise<Blob> {
  if (!file.type.startsWith("image/") || file.type === "image/gif" || file.type === "image/svg+xml") return file;
  if (typeof createImageBitmap !== "function") return file;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    return file;
  }
  try {
    const longer = Math.max(bitmap.width, bitmap.height);
    if (longer <= maxDim) return file;
    const scale = maxDim / longer;
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, "image/jpeg", quality));
    return blob && blob.size < file.size ? blob : file;
  } finally {
    bitmap.close();
  }
}
