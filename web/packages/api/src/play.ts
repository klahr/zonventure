// Playing a graph adventure: the player endpoints and the designer's
// simulator. Both return the same View and Result — the Go types in
// internal/graph/session — because both run the same engine, and one
// component (@zonventure/play-ui) renders either.
//
// Routes: internal/server/routes_player.go (/api/player/play) and
// internal/server/routes_admin.go (/simulate).

import { get, post, type RequestOptions } from "./http.ts";

export type NodeType = "start" | "puzzle" | "story" | "hub" | "gate" | "effect" | "end";

/** internal/graph: Zone */
export interface PlayZone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
  polygon?: [number, number][];
}

/** session.StartOption — a start the team may begin at. */
export interface StartOption {
  node: string;
  title?: string;
  zone?: PlayZone;
  /** The team is inside the zone (always true without one). */
  here: boolean;
}

/** session.Input — the shape of a puzzle's answer box. */
export interface PlayInput {
  kind: "text" | "number";
  /** Set when the answer is a list: how many items to give. */
  parts?: number;
  ordered?: boolean;
}

/** internal/graph: Media. Fetched from mediaURL(id). */
export interface PlayMedia {
  id: number;
  kind: "image" | "audio";
  alt?: string;
}

export const mediaURL = (id: number) => `/media/${id}`;

/** session.HintView — one revealed hint. */
export interface HintView {
  text: string;
  media?: PlayMedia[];
}

/** session.NextHint */
export interface NextHint {
  index: number;
  available_at: string;
  cost?: number;
}

/** session.OfferEntry — one hub branch. */
export interface OfferEntry {
  port: string;
  label?: string;
  locked?: boolean;
}

export interface ItemView {
  slug: string;
  name?: string;
  icon?: string;
  qty: number;
}

export interface JournalView {
  slug: string;
  title?: string;
  body?: string;
}

/**
 * session.View — what the player sees. Every time in it is relative to
 * `now`, the engine's clock, not the device's.
 */
export interface View {
  node?: string;
  type?: NodeType;
  title?: string;
  text?: string;
  zone?: PlayZone;
  here?: boolean;
  /** Set while the team chooses where to begin; `node` is then empty. */
  starts?: StartOption[];
  media?: PlayMedia[];
  input?: PlayInput;
  hints?: HintView[];
  hint_count?: number;
  next_hint?: NextHint;
  attempts?: number;
  offer?: OfferEntry[];
  can_finish?: boolean;
  can_skip?: boolean;
  blocked_until?: string;
  deadline?: string;
  has_timeout?: boolean;
  score: number;
  penalty_score: number;
  penalty_seconds: number;
  finished?: boolean;
  outcome?: "complete" | "fail";
  items?: ItemView[];
  journal?: JournalView[];
  theme?: string;
  now: string;
}

/** session.Result — what one action produced. */
export interface Result {
  accepted: boolean;
  port?: string;
  points?: number;
  response?: string;
  blocked_until?: string;
  hint?: string;
  view: View;
}

/** One thing a player can do. `node` is the step they are looking at. */
export type PlayAction =
  | { kind: "begin"; node: string; port: string }
  | { kind: "answer"; node: string; guess: string }
  | { kind: "continue"; node: string }
  | { kind: "choose"; node: string; port: string }
  | { kind: "skip"; node: string }
  | { kind: "timeout"; node: string }
  | { kind: "hint"; node: string; index: number };

/**
 * Refusal codes (session.Code, plus the player handler's own). A client
 * branches on these, never on the English message.
 */
export type RefusalCode =
  | "stale"
  | "not_here"
  | "locked"
  | "time_up"
  | "not_allowed"
  | "finished"
  | "wrong_node"
  | "paused"
  | "not_running"
  | "no_game"
  | "graph";

/**
 * internal/adventure: Colors — the adventure's brand palette, #rrggbb
 * each, any of them optional. The same palette the lane player used.
 */
export interface Brand {
  accent?: string;
  accent_secondary?: string;
  background?: string;
  text?: string;
  /** The answer bar at the bottom of the screen. */
  submit_bg?: string;
}

// --- player ---------------------------------------------------------------

/** internal/player: PlayState */
export interface PlayState {
  /** "lanes" means this event is not played in the React app. */
  engine: "graph" | "lanes";
  team: { id: number; name: string; token: string };
  event: {
    id: number;
    state: "scheduled" | "running" | "paused" | "completed" | "cancelled";
    scheduled_at?: string;
    adventure_title?: string;
    org_name?: string;
    org_logo_url?: string;
    /** The author's welcome and farewell, Markdown. */
    intro_text?: string;
    outro_text?: string;
    /** Where the team should meet. */
    start?: { lat: number; lng: number; label?: string };
  };
  /** The adventure's palette, when the author set one. */
  brand?: Brand;
  view?: View;
  /** Media ids the team may fetch now, for downloading ahead of need. */
  prefetch?: number[];
}

export const getPlay = (opts?: RequestOptions) => get<PlayState>("/api/player/play", opts);

/**
 * Send one action. The client_action_id makes a retry safe: the server
 * replays its first response instead of running the action twice.
 */
export function sendAction(action: PlayAction, clientActionId: string) {
  const { kind, ...body } = action;
  return post<Result>(`/api/player/play/${kind}`, { ...body, client_action_id: clientActionId });
}

// --- designer simulator -----------------------------------------------------

/**
 * The simulated team. Opaque to the client: it is handed back verbatim
 * on the next call, and the server alone interprets it.
 */
export type SimTeam = Record<string, unknown> & { current?: string; now?: string };

export type SimAction =
  | PlayAction
  | { kind: "start" }
  | { kind: "place"; node: string }
  | { kind: "wait"; seconds: number }
  | { kind: "move"; zone?: string };

export interface SimResponse {
  team: SimTeam | null;
  result: Result;
  /** Every node the step passed through, including where it landed. */
  passed?: string[];
  refusal?: { code: RefusalCode; message: string };
}

export const simulate = <Env>(slug: string, envelope: Env, team: SimTeam | null, action: SimAction, signal?: AbortSignal) =>
  post<SimResponse>(
    `/api/admin/adventures/${encodeURIComponent(slug)}/simulate`,
    { envelope, ...(team ? { team } : {}), action },
    signal ? { signal } : {},
  );

// --- location ----------------------------------------------------------------

/** internal/player: LocationRequest */
export interface LocationPing {
  lat: number;
  lng: number;
  accuracy?: number;
  speed?: number;
  heading?: number;
  client_ts?: string;
}

/**
 * Report where the device is. The server applies its own plausibility
 * and anti-teleport rules; a ping it does not trust is simply not used
 * for zones.
 */
export const postLocation = (ping: LocationPing) => post<void>("/api/player/location", ping);
