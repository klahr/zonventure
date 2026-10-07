import { request, type play } from "@zonventure/api";

/** A station card: a zone of the adventure, as a card the host hides. */
export interface Station {
  zone: string;
  name: string;
  code: string;
  url: string;
}

/** A team of the game being played (or the last one). */
export interface Team {
  id: number;
  name: string;
  headcount: number;
  finished: boolean;
  score: number;
}

/** One game the event has played or is playing. */
export interface Round {
  round: number;
  started_at: string;
  finished_at?: string;
  teams: number;
  players: number;
}

/** A word the host fills in: internal/admin/party_adapt.go hostValueDTO. */
export interface HostValue {
  slug: string;
  label: string;
  default: string;
  value: string;
}

/** A host's version of a text or quiz question. */
export interface BlockOverride {
  text?: string;
  options?: string[];
  answer?: string;
}

/** Something the host may rewrite: a text, a quiz question, or a typed answer (no kind). */
export interface Editable {
  key: string;
  node: string;
  step: string;
  kind?: "text" | "quiz";
  text?: string;
  options?: string[];
  answer?: string;
  accepts?: string[];
  mine?: BlockOverride;
  my_accepts?: string[];
}

/** One of the adventure's places, for an event in a park. */
export interface Place {
  slug: string;
  name: string;
  movable: boolean;
  lat: number;
  lng: number;
  radius_m: number;
  placed: boolean;
}

/** A hosted event as its host sees it: internal/admin/party.go hostPartyDTO. */
export interface HostedEvent {
  name: string;
  adventure_title: string;
  org_name: string;
  state: "scheduled" | "running" | "paused" | "completed" | "cancelled";
  kids: number;
  valid_until: string;
  expired: boolean;
  stations: Station[];
  live_url: string;
  play_url: string;
  player_url: string;
  round: number;
  round_open: boolean;
  teams_limit: number;
  /** The most teams a game can be bought up to. */
  max_teams: number;
  /** What one more block of teams costs (block_cents 0: free). */
  price: { block_cents: number; currency: string; teams_per_block: number; free_teams: number };
  /** A quiz trail: its cards print as a PDF; reprint when they changed since. */
  trail: boolean;
  reprint: boolean;
  /** A quiz trail made at the public door: the host link edits it, and from keep_from it may be kept. */
  public_trail?: boolean;
  keep_from?: string;
  first_game_at?: string;
  teams: Team[];
  rounds: Round[];
  mode: "qr" | "park";
  cards_ok: boolean;
  park_ok: boolean;
  values: HostValue[];
  editables: Editable[];
  places: Place[];
  /** The adventure's look, starter theme and cover picture, as the join page shows them. */
  look?: play.Brand;
  theme?: string;
  cover_url?: string;
}

export interface Personalisation {
  values: Record<string, string>;
  blocks: Record<string, BlockOverride>;
  answers: Record<string, string[]>;
}

/**
 * What the hosted views call. The host page and the console reach the
 * same handlers under different bases: /api/party/{token} by host link,
 * /api/admin/events/{id}/hosted by the arranger's session.
 */
/** A purchase's answer: the checkout to pay at, or nothing to pay. */
export interface Checkout {
  checkout_url?: string;
}

export interface HostedApi {
  get(): Promise<HostedEvent>;
  /** Buys blocks more blocks of teams per game. */
  addBlocks(blocks: number): Promise<Checkout>;
  finishGame(): Promise<void>;
  sendPlayerLink(email: string): Promise<void>;
  personalise(body: Personalisation): Promise<void>;
  setMode(mode: "qr" | "park"): Promise<void>;
  placeAt(slug: string, lat: number, lng: number): Promise<void>;
  shiftTo(lat: number, lng: number): Promise<void>;
  /** The URL of a QR code image: a station card's, or the player link's. */
  qr(station?: string): string;
  /** The URL of a quiz trail's cards PDF, or its answer key. */
  pdf(answers?: boolean): string;
  /** Takes a team out of the game; absent where the page has its own way. */
  removeTeam?: (id: number) => Promise<void>;
  /** Pauses or resumes the game; absent where the page has its own controls. */
  action?: (action: "pause" | "resume") => Promise<void>;
}

const enc = encodeURIComponent;

/** The API under base, as the host page and the console share it. */
export function hostedApi(base: string, extra: Pick<HostedApi, "removeTeam" | "action"> = {}): HostedApi {
  return {
    get: () => request<HostedEvent>(base),
    addBlocks: (blocks) => request<Checkout>(`${base}/blocks`, { method: "POST", body: { blocks } }),
    finishGame: () => request<void>(`${base}/finish-game`, { method: "POST" }),
    sendPlayerLink: (email) => request<void>(`${base}/send-player-link`, { method: "POST", body: { email } }),
    personalise: (body) => request<void>(`${base}/personalise`, { method: "PUT", body }),
    setMode: (mode) => request<void>(`${base}/mode`, { method: "PUT", body: { mode } }),
    placeAt: (slug, lat, lng) => request<void>(`${base}/places/${enc(slug)}`, { method: "PUT", body: { lat, lng } }),
    shiftTo: (lat, lng) => request<void>(`${base}/places/shift`, { method: "POST", body: { lat, lng } }),
    qr: (station) => `${base}/qr` + (station ? `?station=${enc(station)}` : ""),
    pdf: (answers) => `${base}/cards.pdf` + (answers ? "?answers=1" : ""),
    ...extra,
  };
}

/** The host page's API, by host link. */
export function partyApi(token: string): HostedApi {
  const base = `/api/party/${enc(token)}`;
  return hostedApi(base, {
    removeTeam: (id) => request<void>(`${base}/teams/${id}`, { method: "DELETE" }),
    action: (action) => request<void>(`${base}/${action}`, { method: "POST" }),
  });
}

/** The console's API for a hosted event. */
export function consoleApi(eventId: number): HostedApi {
  return hostedApi(`/api/admin/events/${eventId}/hosted`);
}

/** Whether what the host sets up may change now: only between games. */
export function adaptable(e: HostedEvent): boolean {
  return !e.round_open && !e.expired && e.state !== "completed" && e.state !== "cancelled";
}

/** A price in minor units, in the reader's locale. */
export function money(cents: number, currency: string, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency: currency.toUpperCase(), maximumFractionDigits: cents % 100 === 0 ? 0 : 2 }).format(cents / 100);
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency.toUpperCase()}`;
  }
}
