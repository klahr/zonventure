import { request } from "@zonventure/api";
import type { Card, TemplateInfo, TrailDoc } from "@zonventure/trail";

/**
 * The public quiz trail: internal/admin/trails_public.go. A draft opens
 * with its secret, which only this browser holds (localStorage), or with
 * an email session from a mailed link.
 */

/** The start page: trailStartDTO. */
export interface TrailStart {
  org_name: string;
  org_slug: string;
  enabled: boolean;
  payments_open: boolean;
  price: { block_cents: number; currency: string; teams_per_block: number };
  play_days: number;
  start_days: number;
  templates: TemplateInfo[];
}

/** A public trail: publicTrailDTO, the console's trail and where it stands. */
export interface PublicTrail extends TrailDoc {
  id: number;
  state: "draft" | "paid" | "playing" | "ended";
  email?: string;
  payments_open: boolean;
  price: { block_cents: number; currency: string; teams_per_block: number };
  valid_until?: string;
  first_game_at?: string;
  round_open: boolean;
}

/** One trail on My trails: trailSummaryDTO. */
export interface TrailSummary {
  id: number;
  title: string;
  state: PublicTrail["state"];
  cards: number;
  created_at: string;
  updated_at: string;
  valid_until?: string;
  first_game_at?: string;
  saved: boolean;
}

/** A draft this browser holds. */
export interface TrailKey {
  id: number;
  secret: string;
}

const enc = encodeURIComponent;
const KEYS = "zv.trails";
const SESSION = "zv.trail-session";

function read<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

function write(key: string, value: unknown) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Private mode: the draft lives as long as the tab, and its link.
  }
}

/** The drafts this browser holds. */
export const heldTrails = (): TrailKey[] => read<TrailKey[]>(KEYS, []).filter((k) => k && typeof k.id === "number" && typeof k.secret === "string");

export function holdTrail(k: TrailKey) {
  write(KEYS, [k, ...heldTrails().filter((x) => x.id !== k.id)]);
}

export function forgetTrails(ids: number[]) {
  write(KEYS, heldTrails().filter((k) => !ids.includes(k.id)));
}

/** The email session, from a confirm or sign-in link. */
export const trailSession = (): { session: string; email: string } | null => read(SESSION, null);
export const setTrailSession = (s: { session: string; email: string } | null) => write(SESSION, s);

/** The headers that open trail id: its secret, else the session. */
export function trailHeaders(id?: number): Record<string, string> {
  const h: Record<string, string> = {};
  const key = id === undefined ? undefined : heldTrails().find((k) => k.id === id);
  if (key) h["X-Trail-Secret"] = key.secret;
  const s = trailSession();
  if (s) h["X-Trail-Session"] = s.session;
  return h;
}

/** Where a trail is edited: by this browser's key, or by host link. */
export const editURL = (id: number) => `/app/party/trail/edit#${id}`;
export const hostEditURL = (token: string) => `/app/party/trail/host#${enc(token)}`;
/** The link to copy: it carries the secret, in the fragment. */
export const shareURL = (k: TrailKey) => `${location.origin}/app/party/trail/edit#${k.id}.${k.secret}`;

export const getStart = (org: string, lang: string) => request<TrailStart>(`/api/o/${enc(org)}/trail?lang=${enc(lang)}`);

export const createDraft = (org: string, template: string, language: string) =>
  request<{ id: number; secret: string; trail: PublicTrail }>(`/api/o/${enc(org)}/trail/drafts`, { method: "POST", body: { template, language } });

const draft = (id: number) => `/api/trail/drafts/${id}`;

export const getDraft = (id: number) => request<PublicTrail>(draft(id), { headers: trailHeaders(id) });

export const saveDraft = (id: number, body: { title: string; cards: Card[]; revision: number }) =>
  request<PublicTrail>(draft(id), { method: "PUT", body, headers: trailHeaders(id) });

export const deleteDraft = (id: number) => request<void>(draft(id), { method: "DELETE", headers: trailHeaders(id) });

export const duplicateDraft = (id: number) =>
  request<{ id: number; secret: string; trail: PublicTrail }>(`${draft(id)}/duplicate`, { method: "POST", headers: trailHeaders(id) });

export const checkout = (id: number, body: { email: string; blocks: number; consent: boolean }) =>
  request<{ checkout_url?: string; host_url?: string }>(`${draft(id)}/checkout`, { method: "POST", body, headers: trailHeaders(id) });

export const sendHostLink = (id: number) => request<void>(`${draft(id)}/host-link`, { method: "POST", headers: trailHeaders(id) });

export const saveToEmail = (email: string) => request<void>("/api/trail/email/save", { method: "POST", body: { email, trails: heldTrails() } });

export const signIn = (email: string) => request<void>("/api/trail/email/signin", { method: "POST", body: { email } });

export const verify = (token: string) => request<{ session: string; email: string }>("/api/trail/email/verify", { method: "POST", body: { token } });

export const listMine = () =>
  request<{ trails: TrailSummary[]; gone: number[]; email: string }>("/api/trail/mine", { method: "POST", body: { trails: heldTrails() }, headers: trailHeaders() });

/** The trail a host link runs. */
export const getHostTrail = (token: string) => request<PublicTrail>(`/api/party/${enc(token)}/cards`);
export const saveHostTrail = (token: string, body: { title: string; cards: Card[]; revision: number }) =>
  request<PublicTrail>(`/api/party/${enc(token)}/cards`, { method: "PUT", body });
export const keepLink = (token: string) => request<{ url: string }>(`/api/party/${enc(token)}/keep`, { method: "POST" });
