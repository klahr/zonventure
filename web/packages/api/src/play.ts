// Playing a graph adventure: the player endpoints and the designer's
// simulator. Both return the same View and Result — the Go types in
// internal/graph/session — because both run the same engine, and one
// component (@zonventure/play-ui) renders either.
//
// Routes: internal/server/routes_player.go (/api/player/play) and
// internal/server/routes_admin.go (/simulate).

import { get, post, request, type RequestOptions } from "./http.ts";

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
  /** "questions": answered by the step's quiz blocks; the dock continues once each is picked. */
  kind: "text" | "number" | "tiles" | "search" | "choice" | "lock" | "questions" | "scan" | "items" | "keys";
  /** "keys": a two-key launch's posts, by name — where keys are turned. Empty: anywhere. */
  posts?: string[];
  /** The role whose player alone answers, by name; not_yours: not this player. */
  by?: string;
  not_yours?: boolean;
  /** The author's answer bar: its placeholder, its button (a scan's too), and no QR button. */
  placeholder?: string;
  button?: string;
  no_scan?: boolean;
  /** A combination lock: how many wheels, their symbols, where each starts. */
  wheels?: number;
  symbols?: { value: string; label?: string; image?: PlayMedia }[];
  start?: number[];
  /** Item slots (kind "items"): each slot's name — never the item it wants. */
  slots?: string[];
  /** The team lacks an item the slots need; missing_items names them if the author chose to. */
  missing?: boolean;
  missing_items?: string[];
  /** Meanwhile the team may leave by the puzzle's route for it; leave_label is that route's label. */
  leave?: boolean;
  leave_label?: string;
  /** A choice: the questions and their options (never the answers). */
  fields?: { label?: string; options: { value: string; label?: string }[] }[];
  /** A search: the zone the team stands in, if any. */
  at?: string;
  /** Set when the answer is a list: how many items to give. */
  parts?: number;
  ordered?: boolean;
  /** A tile puzzle (kind "tiles"): the picture and the grid to cut it into. */
  rows?: number;
  cols?: number;
  image?: PlayMedia;
}

/** internal/graph: Media. Fetched from mediaURL(id). */
export interface PlayMedia {
  id: number;
  kind: "image" | "audio";
  alt?: string;
}

export const mediaURL = (id: number) => `/media/${id}`;

/**
 * session.BlockView — one piece of a step's content, in order, with
 * what the server works out for it. `locked` is a teaser for a block
 * whose condition does not hold yet: it carries only its id and kind,
 * and `place` when it waits for the team to reach a zone.
 */
export type PlayBlock = PlayBlockBody & {
  id: string;
  locked?: boolean;
  place?: PlayZone;
};

export type PlayBlockBody =
  | { kind: "text"; text: string }
  | { kind: "image" | "video"; media: number; alt?: string }
  | { kind: "audio"; media: number; alt?: string; reverse?: boolean; spectrogram?: boolean }
  | { kind: "compass"; zone: string; hide_distance?: boolean }
  | { kind: "map"; zone: string; zoom?: number; hide_team?: boolean; markers?: { lat: number; lng: number; label?: string }[] }
  | { kind: "note"; text: string; style?: string }
  | { kind: "chat"; lines: { who?: string; text: string; me?: boolean }[]; media?: number; alt?: string }
  | { kind: "cipher"; cipher: string; shift?: number; key?: string; wheel?: boolean }
  | { kind: "hotspots"; media: number; alt?: string; spots?: { x: number; y: number; text: string }[] }
  | { kind: "divider"; style?: string }
  | { kind: "radar"; zone: string }
  | { kind: "overlay"; media: number; alt?: string }
  /** style: "" a torch in the dark, "scratch" a scratch card, "uv" a UV lens, "red" a red lens. */
  | { kind: "torch"; media: number; alt?: string; style?: string }
  | { kind: "level"; pitch?: number; text?: string }
  | { kind: "noise"; threshold?: number; text?: string }
  | { kind: "countdown"; seconds: number; from?: string; text?: string; ends_at?: string }
  | { kind: "status"; items?: boolean; counters?: string[]; counter_values?: { slug: string; name?: string; value: number }[] }
  | { kind: "standings"; standings?: Standings }
  /** text is the step's title, in the team's language. */
  | { kind: "title"; text?: string }
  /** points is what the puzzle's best answer is worth. */
  | { kind: "points"; points?: number }
  /**
   * picked is the team's answer; final once it has left the step, before
   * which it may change. answer comes only when the team has finished.
   */
  | { kind: "quiz"; text: string; options: string[]; worth?: number; picked?: string; final?: boolean; answer?: string }
  | { kind: "tiebreak"; text: string; picked?: string; final?: boolean; target?: number }
  /** parts are this player's only; player is which of players this phone is, from 1. The simulator sends every part. */
  | { kind: "split"; parts: string[]; player?: number; players?: number }
  | { kind: "bearing"; heading?: number; text?: string }
  | { kind: "call"; media: number; alt?: string; who?: string }
  /** picked is the struck cards' indexes, "0,2". */
  | { kind: "suspects"; cards: { name: string; text?: string; media?: number; alt?: string }[]; picked?: string }
  /** photo is the team's latest picture for this challenge, and its review. */
  | { kind: "photo"; text: string; worth?: number; photo?: PhotoView }
  | { kind: "spotdiff"; media: number; other?: number; alt?: string; spots?: { x: number; y: number; text: string }[] }
  /** speed in words a minute; absent is 8. */
  | { kind: "flasher"; text: string; speed?: number }
  | { kind: "letters"; text: string }
  | { kind: "shake"; threshold?: number; text?: string }
  /** tally counts votes per option; voted is this player's option index ("" none); voters is the team's players. */
  | { kind: "vote"; text: string; options: string[]; tally?: number[]; voted?: string; voters?: number }
  | { kind: "bulletin" }
  /** The cards stay face down: deck is how many, and once drawn picked is its index and dealt the card. */
  | { kind: "draw"; text?: string; deck?: number; picked?: string; dealt?: string }
  /** Only the saboteur's phone gets it; picked "1" once pressed. */
  | { kind: "sabotage"; text?: string; worth?: number; seconds?: number; picked?: string }
  /** suspects are this player's teammates, tally counts each's votes in that order, voted the slot named. */
  | { kind: "accuse"; text: string; worth?: number; suspects?: { slot: number; name: string }[]; tally?: number[]; voted?: string; voters?: number }
  /** Drawn from the view's roles: the player's own, or with team everyone's. */
  | { kind: "roles"; text?: string; team?: boolean; choose?: boolean; lock?: boolean };

/** session.PhotoView */
export interface PhotoView {
  id: number;
  status: "pending" | "approved" | "denied";
}

/** session.QuizResult */
export interface QuizResult {
  right: number;
  total: number;
  points: number;
  items: { node: string; block: string; question: string; options: string[]; picked?: string; answer: string }[];
  tiebreak?: { question: string; guess?: number; answer: number };
}

/** session.Standings — the team's place among the event's teams. */
export interface Standings {
  rank: number;
  of: number;
  ahead?: { name: string; gap: number };
  behind?: { name: string; gap: number };
}

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
  /** A home-screen branch the team has been into; still open. */
  visited?: boolean;
  note?: string;
  image?: PlayMedia;
}

export interface ItemView {
  slug: string;
  name?: string;
  icon?: string;
  qty: number;
  /** The role whose kit it is from, by name; not_mine: this player cannot use it. */
  role?: string;
  not_mine?: boolean;
  /** The player it was handed to, by name, if it was; give: this player may hand it on. */
  holder?: string;
  give?: boolean;
}

/** A code that hands an item to whichever teammate scans it, and its picture, until expires_at. */
export interface ItemGift {
  code: string;
  qr: string;
  expires_at: string;
}

/** session.RevealView: a secret role unmasked at the end. dealt is false when no one held it. */
export interface RevealView {
  role: string;
  name?: string;
  dealt: boolean;
  holder?: string;
  goals?: { text: string; met: boolean }[];
  caught?: boolean;
}

/** session.RoleView: one role, and who holds it — claimed, or dealt. */
export interface RoleView {
  slug: string;
  name?: string;
  text?: string;
  image?: PlayMedia;
  /** A saboteur's, listed only to its player. */
  secret?: boolean;
  /** This phone's player holds it. */
  mine?: boolean;
  /** The name of the player who holds it. */
  holder?: string;
  /** They chose it, rather than being dealt it. */
  claimed?: boolean;
  /** The team's roles are locked: no one takes or gives one back. */
  final?: boolean;
}

export interface JournalView {
  slug: string;
  title?: string;
  body?: string;
  section?: string;
  media?: PlayMedia[];
}

/**
 * session.View — what the player sees. Every time in it is relative to
 * `now`, the engine's clock, not the device's.
 */
export interface View {
  node?: string;
  type?: NodeType;
  title?: string;
  /** A hub's text. On other steps, blocks' texts flattened, for older bundles. */
  text?: string;
  zone?: PlayZone;
  here?: boolean;
  /** Set while the team chooses where to begin; `node` is then empty. */
  starts?: StartOption[];
  /** What a puzzle, story or end shows, in order. */
  blocks?: PlayBlock[];
  /** How the team did on the quiz walk, once it has finished. */
  quiz?: QuizResult;
  /** When a hidden block could appear by itself: ask again then. */
  recheck_at?: string;
  /** A hidden block waits for the team to reach a zone: report position often. */
  watch_position?: boolean;
  /** blocks' pictures and sounds flattened, for older bundles. */
  media?: PlayMedia[];
  input?: PlayInput;
  hints?: HintView[];
  hint_count?: number;
  next_hint?: NextHint;
  attempts?: number;
  offer?: OfferEntry[];
  can_finish?: boolean;
  /** Languages the adventure plays in, main first, and the view's own. */
  languages?: string[];
  lang?: string;
  /** When the adventure's time limit falls, for a countdown. */
  ends_at?: string;
  /** The adventure's time limit has moved the team. */
  timed_out?: boolean;
  /** The author's message for that; empty for the player's own default. */
  time_up_text?: string;
  /** The home screen a team may go back to from here, and its title. */
  back_to?: string;
  back_label?: string;
  can_skip?: boolean;
  blocked_until?: string;
  deadline?: string;
  has_timeout?: boolean;
  score: number;
  penalty_score: number;
  /** The adventure keeps the running score off the phone. */
  hide_score?: boolean;
  penalty_seconds: number;
  finished?: boolean;
  outcome?: "complete" | "fail";
  items?: ItemView[];
  journal?: JournalView[];
  /** A puzzle done together: keys turned, players agreed. */
  together?: Together;
  /** The adventure's roles, and who on the team holds each. */
  roles?: RoleView[];
  /** Every player has chosen a role, or every role is chosen; absent where no one chooses. */
  roles_chosen?: boolean;
  /** Once the team has finished: who held each secret role, their goals, and whether the team named them. */
  reveal?: RevealView[];
  /** The adventure keeps a journal, so its button shows even while empty. */
  has_journal?: boolean;
  theme?: string;
  now: string;
}

/** session.Result — what one action produced. */
/** session.Together: how far the players are with a puzzle they do together. */
export interface Together {
  kind: "keys" | "agree";
  done: number;
  of: number;
  /** This phone's player has turned their key, or given the answer. */
  mine?: boolean;
  /** When the first counted key stops counting. */
  until?: string;
}

export interface Result {
  accepted: boolean;
  /** The player's part is in; the step waits for their teammates. */
  waiting?: Together;
  port?: string;
  points?: number;
  response?: string;
  blocked_until?: string;
  hint?: string;
  /** The time limit moved the team instead of the action asked for. */
  time_up?: boolean;
  view: View;
}

/** One thing a player can do. `node` is the step they are looking at. */
export type PlayAction =
  | { kind: "begin"; node: string; port: string }
  | { kind: "answer"; node: string; guess: string }
  | { kind: "continue"; node: string }
  | { kind: "back"; node: string }
  | { kind: "timeup" }
  | { kind: "language"; lang: string }
  | { kind: "choose"; node: string; port: string }
  | { kind: "skip"; node: string }
  /** Solve a puzzle with the adventure's cheat password. Never queued: the password must not be stored. */
  | { kind: "cheat"; node: string; password: string }
  /** Leave an item puzzle to find what its slots need. */
  | { kind: "leave"; node: string }
  /** This player's key in a two-key launch. */
  | { kind: "turn"; node: string }
  | { kind: "timeout"; node: string }
  | { kind: "hint"; node: string; index: number }
  /**
   * Answer a quiz question with 1, X or 2, or the tie-breaker with a
   * number (final once the team leaves); strike a suspect board's cards
   * ("0,2"); or draw a card (choice ignored).
   */
  | { kind: "pick"; node: string; block: string; choice: string }
  /** This player's vote on a vote block: the option's index. Live events only. */
  | { kind: "vote"; node: string; block: string; choice: string }
  /** The saboteur presses a sabotage block. Live events only. */
  | { kind: "sabotage"; node: string; block: string };

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
  | "unanswered"
  | "paused"
  | "not_running"
  | "no_game"
  | "bad_password"
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
  /** A typeface key from adventure.Fonts; empty is the default. */
  font?: string;
  /** Titles' typeface, from the same list; empty follows font. */
  heading_font?: string;
  /** Corner style; empty is "rounded". */
  corners?: "rounded" | "soft" | "sharp";
  /** How panels stand off the page: a shadow, or a tinted fill. Empty is "raised". */
  cards?: "raised" | "flat";
  /** A clear line round cards, raised or flat. */
  card_outline?: boolean;
}

// --- player ---------------------------------------------------------------

/** internal/player: PlayState */
export interface PlayState {
  /**
   * avatar_url is the team's picture, if it has one. locked: the team
   * has begun, so its players can no longer change their names or the
   * picture (the organisers still can).
   */
  team: { id: number; name: string; token: string; avatar_url?: string; locked?: boolean };
  /** The signed-in player: one slot of the team. */
  me?: { id: number; name?: string };
  event: {
    id: number;
    state: "scheduled" | "running" | "paused" | "completed" | "cancelled";
    scheduled_at?: string;
    /** The server's clock when this state was built, for the lobby's countdown to scheduled_at. */
    server_now?: string;
    /** The adventure has a cheat password, so the phone offers a way to enter it. */
    cheat?: boolean;
    adventure_title?: string;
    org_name?: string;
    org_logo_url?: string;
    /** The author's welcome and farewell, Markdown. */
    intro_text?: string;
    outro_text?: string;
    /** Where the team should meet. */
    start?: { lat: number; lng: number; label?: string };
  };
  /** Set once the team has finished. */
  finish?: Finish;
  /** The adventure's palette, when the author set one. */
  brand?: Brand;
  /** The running organisation's colours: the app's own chrome, and steps of an adventure without a palette. */
  org_brand?: Brand;
  view?: View;
  /** Media ids the team may fetch now, for downloading ahead of need. */
  prefetch?: number[];
  /** What the organisers have told this team, newest first. Beside the game, not in it. */
  messages: EventMessage[];
}

/** player.PlayFinish — what the end screen tells a finished team. */
export interface Finish {
  /** Place among the event's started teams, of `teams`; can still change while others play. */
  rank?: number;
  teams?: number;
  /** Start to finish. */
  seconds?: number;
  /** The team's recap page; it opens once the event ends. */
  recap_url?: string;
}

/** eventmsg.Message — one message from the organisers. */
export interface EventMessage {
  id: number;
  text: string;
  created_at: string;
}

export const getPlay = (opts?: RequestOptions) => get<PlayState>("/api/player/play", opts);

/** Send the team's picture for a photo block. It waits for the organisers' review. */
export function uploadPhoto(node: string, block: string, file: Blob) {
  const form = new FormData();
  form.append("node", node);
  form.append("block", block);
  form.append("file", file);
  return request<{ photo: PhotoView }>("/api/player/play/photo", { method: "POST", form });
}

/** Set the team's picture; any member may, until the team begins (409 "team_started" after). */
export function setTeamAvatar(file: Blob) {
  const form = new FormData();
  form.append("file", file);
  return request<{ url: string }>("/api/player/team/avatar", { method: "POST", form });
}

/** Remove the team's picture, on the same terms. */
export const clearTeamAvatar = () => request<void>("/api/player/team/avatar", { method: "DELETE" });

/** A picture the team took, for its own phones. */
export const photoURL = (id: number) => `/api/player/play/photo/${id}`;

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
  | { kind: "move"; zone?: string; lat?: number; lng?: number }
  /** Take a puzzle output without its answer — the simulator's pass. */
  | { kind: "force"; node: string; port: string };

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

/** internal/sim: Config */
export interface EventSimConfig {
  teams: number;
  members: number;
  /** 0, all teams alike, to 1, very different. */
  spread: number;
  /** Seconds between the arranger's messages; 0 sends none. */
  message_every: number;
  max_seconds: number;
  start_spread: number;
  /** RFC 3339; time-of-day conditions read it. */
  start?: string;
  seed: number;
}

/** internal/sim: Event */
export interface EventSimEvent {
  t: number;
  /** -1 for a message to every team. */
  team: number;
  kind: string;
  node?: string;
  text?: string;
}

/** internal/sim: Team */
export interface EventSimTeam {
  name: string;
  members: number;
  path: { t: number; lat: number; lng: number }[] | null;
  pace: number;
  skill: number;
  patience: number;
  finished?: number;
  stalled?: boolean;
  outcome?: string;
  score: number;
  walked: number;
  wrong: number;
  hints: number;
  skips: number;
  nudges: number;
  steps: number;
  progress: { t: number; score: number; steps: number }[] | null;
}

/** internal/sim: Step */
export interface EventSimStep {
  node: string;
  title?: string;
  type: NodeType;
  reached: number;
  left: number;
  median: number;
  max: number;
  wrong: number;
  hints: number;
  skips: number;
  timeouts: number;
  nudges: number;
  forced: number;
  waiting: number;
}

/** internal/sim: Result */
export interface EventSimResult {
  start: string;
  duration: number;
  ended: "finished" | "time" | "stalled";
  teams: EventSimTeam[];
  events: EventSimEvent[] | null;
  steps: EventSimStep[] | null;
}

export const simulateEvent = <Env>(slug: string, envelope: Env, config: EventSimConfig, signal?: AbortSignal) =>
  post<EventSimResult>(`/api/admin/adventures/${encodeURIComponent(slug)}/simulate-event`, { envelope, config }, signal ? { signal } : {});

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

/** Check a cheat password before turning cheating on. 403 bad_password when it is wrong. */
export const unlockCheat = (password: string) => post<void>("/api/player/play/unlock", { password });

/** Put the player's own name on the team (the organiser's is often "Player 3"). */
export const renameMe = (name: string) => post<{ name: string }>("/api/player/me/name", { name });
/** Takes a role, or with claim false gives it back to be dealt again. 409 role_taken: a teammate claimed it. */
export const claimRole = (role: string, claim: boolean) => post<void>("/api/player/role", { role, claim });
/** A code handing one of the player's kit items to a teammate. 409 not_yours: theirs to give, not this player's. */
export const giveItem = (item: string) => post<ItemGift>("/api/player/items/give", { item });
/** Takes the item a teammate's code gives. 404 gift_unknown, 410 gift_expired, 409 gift_own or gift_gone. */
export const receiveItem = (code: string) => post<{ item: string; name?: string; icon?: string }>("/api/player/items/receive", { code });

/** Rename the team; any member may. 409 when another team in the event has the name. */
export const renameTeam = (name: string) => post<{ name: string }>("/api/player/team/name", { name });

/** Sign this phone out. The link still works to sign in again. */
export const logout = () => post<void>("/api/player/logout", {});

/** Erase the player's pings, answers and photos, and revoke their link. */
export const forget = () => post<void>("/api/player/forget", {});

/** Mail fresh links to a team's contact address. Always 204, whether it matched or not. */
export const recoverLink = (email: string) => post<void>("/api/player/recover-link", { email });
