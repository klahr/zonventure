// Adventure and graph endpoints. Routes: internal/server/routes_admin.go;
// handlers: internal/admin/handlers.go (list) and internal/admin/graph.go.

import { ApiError, get, patch, post, put, request } from "./http.ts";
import type { Brand } from "./play.ts";

/** internal/admin/handlers.go: adventureDTO */
export interface AdventureSummary {
  slug: string;
  title: string;
  description?: string;
  author?: string;
  page_count: number;
  org_id?: number;
  /** Whether the caller may edit it — and so open it in the designer. */
  mine_to_edit: boolean;
  avatar_url?: string;
  published: boolean;
  shared: boolean;
  language: string;
}

export const listAdventures = (signal?: AbortSignal) =>
  get<{ adventures: AdventureSummary[] }>("/api/admin/adventures", signal ? { signal } : {});

/**
 * The graph payloads, left generic so this package does not depend on
 * @zonventure/graph. The designer supplies the concrete types.
 */
export interface GraphPayload<Env, Rep> {
  version?: number;
  envelope: Env;
  report: Rep;
  /** The live version, when anything is published. */
  published?: { version: number; label?: string };
  /** The draft differs from what is published — "unpublished changes". */
  dirty?: boolean;
  saved_at?: string;
}

/** internal/graphstore: Version — one release in the history. */
export interface GraphVersion {
  version: number;
  label?: string;
  created_at: string;
  author?: string;
  published?: boolean;
}

const adventure = (slug: string) => `/api/admin/adventures/${encodeURIComponent(slug)}`;
const base = (slug: string) => `${adventure(slug)}/graph`;

/** The latest saved graph, or null when the adventure has none yet. */
export async function getGraph<Env, Rep>(slug: string, signal?: AbortSignal) {
  try {
    return await get<GraphPayload<Env, Rep>>(base(slug), signal ? { signal } : {});
  } catch (e) {
    // "No graph yet" is a normal state for an adventure still on lanes,
    // not a failure: the designer offers to convert or start empty.
    if (e instanceof ApiError && e.code === "graph_not_found") return null;
    throw e;
  }
}

/**
 * Autosave the draft. Errors do not block; the report lists them. With
 * `since` — the saved_at the draft was loaded or last saved at — a save
 * that would overwrite someone else's later one is refused (409
 * draft_changed) instead.
 */
export const saveGraph = <Env, Rep>(slug: string, envelope: Env, since?: string) =>
  put<GraphPayload<Env, Rep>>(base(slug), envelope, since ? { headers: { "X-Draft-Base": since } } : {});

/** Release the saved draft. Refused (422 graph_invalid) when it has errors. */
export const publishGraph = <Env, Rep>(slug: string, label: string) =>
  post<GraphPayload<Env, Rep>>(base(slug) + "/publish", label ? { label } : undefined);

export const listVersions = (slug: string, signal?: AbortSignal) =>
  get<{ versions: GraphVersion[] }>(base(slug) + "/versions", signal ? { signal } : {});

export const getVersion = <Env, Rep>(slug: string, version: number) =>
  get<GraphPayload<Env, Rep>>(`${base(slug)}/versions/${version}`);

/** Roll back: make an older version the published one. */
export const publishVersion = <Env, Rep>(slug: string, version: number, label = "") =>
  post<GraphPayload<Env, Rep>>(`${base(slug)}/versions/${version}/publish`, label ? { label } : undefined);

export const labelVersion = (slug: string, version: number, label: string) =>
  patch<void>(`${base(slug)}/versions/${version}`, { label });

export const validateGraph = <Env, Rep>(slug: string, envelope: Env, signal?: AbortSignal) =>
  post<GraphPayload<Env, Rep>>(base(slug) + "/validate", envelope, signal ? { signal } : {});

/**
 * The report carried by a refused save (422 graph_invalid), so the
 * designer can mark every problem rather than just the first.
 */
export function refusedReport<Rep>(e: unknown): Rep | undefined {
  if (e instanceof ApiError && e.code === "graph_invalid") {
    return (e.body as { report?: Rep } | undefined)?.report;
  }
  return undefined;
}

// --- look ------------------------------------------------------------------

/** internal/admin: adventureEditDTO, the palette part. */
interface AdventureEdit {
  title: string;
  intro_text: string;
  outro_text: string;
  language: string;
  accent_color: string;
  accent_secondary_color: string;
  background_color: string;
  text_color: string;
  submit_bg_color: string;
  font?: string;
  heading_font?: string;
  corners?: string;
  cards?: string;
  card_outline?: boolean;
}

/**
 * The adventure's palette — the colours the player paints a step with.
 * It belongs to the adventure, not to a graph version: recolouring does
 * not make a new version, and applies to events already running.
 */
export async function getLook(slug: string, signal?: AbortSignal): Promise<Look> {
  const a = await get<AdventureEdit>(`${adventure(slug)}/edit`, signal ? { signal } : {});
  const b: Brand = {};
  if (a.accent_color) b.accent = a.accent_color;
  if (a.accent_secondary_color) b.accent_secondary = a.accent_secondary_color;
  if (a.background_color) b.background = a.background_color;
  if (a.text_color) b.text = a.text_color;
  if (a.submit_bg_color) b.submit_bg = a.submit_bg_color;
  if (a.font) b.font = a.font;
  if (a.heading_font) b.heading_font = a.heading_font;
  if (a.corners === "rounded" || a.corners === "soft" || a.corners === "sharp") b.corners = a.corners;
  if (a.cards === "raised" || a.cards === "flat") b.cards = a.cards;
  if (a.card_outline) b.card_outline = true;
  return { title: a.title, intro: a.intro_text, outro: a.outro_text, language: a.language, brand: b };
}

/** The adventure's own settings the designer shows beside its graph. */
export interface Look {
  title: string;
  intro: string;
  outro: string;
  /** The language the adventure is written in — its graph's main language. */
  language: string;
  brand: Brand;
}

/** Set the language the adventure is written in. */
export const saveLanguage = (slug: string, language: string) =>
  patch<void>(adventure(slug), { language });

/** Save the palette. A missing colour is cleared back to the default. */
export const saveBrand = (slug: string, b: Brand) =>
  patch<void>(adventure(slug), {
    accent_color: b.accent ?? "",
    accent_secondary_color: b.accent_secondary ?? "",
    background_color: b.background ?? "",
    text_color: b.text ?? "",
    submit_bg_color: b.submit_bg ?? "",
    font: b.font ?? "",
    heading_font: b.heading_font ?? "",
    corners: b.corners ?? "",
    cards: b.cards ?? "",
    card_outline: !!b.card_outline,
  });

// --- media -----------------------------------------------------------------

/** internal/admin: mediaView */
export interface MediaUpload {
  id: number;
  kind: "image" | "audio" | "video";
  mime_type: string;
  size_bytes: number;
}

/**
 * Upload one image or audio file for the adventure. Images are resized
 * by the server; the same file twice returns the same id.
 */
export function uploadMedia(slug: string, file: File) {
  const form = new FormData();
  form.append("file", file);
  return request<MediaUpload>(`${adventure(slug)}/media`, { method: "POST", form });
}

// --- working together (internal/admin/collab.go) ---------------------------

export interface UserRef {
  id: number;
  name: string;
  /** Their picture, absent while they have none. */
  avatar?: string;
}

export interface Comment {
  id: number;
  author?: UserRef;
  /** Written by an assistant, on its author's behalf. */
  assistant: boolean;
  body: string;
  created_at: string;
  edited_at?: string;
  /** Who it @mentions. */
  mentions: number[];
}

/** A conversation on a step (node), a block of one, or with node "" the adventure. */
export interface Thread {
  id: number;
  node: string;
  block_id?: string;
  created_by?: UserRef;
  created_at: string;
  resolved_at?: string;
  resolved_by?: UserRef;
  /** It asks the assistant (@assistant): a task until resolved. */
  for_assistant: boolean;
  comments: Comment[];
  /** A proposed change to the step: as the suggester found it, and as they would have it. */
  suggestion?: { base: unknown; proposed: unknown };
  suggestion_state?: "open" | "accepted" | "rejected";
}

/** The fixed set of reactions. */
export const REACTIONS = ["👍", "❤️", "🔥", "😕", "🎉", "👀"] as const;

/** Everyone's emoji on a comment ("comment:<id>"). */
export interface Reaction {
  target: string;
  emoji: string;
  users: number[];
}

export type StepStatus = "" | "idea" | "draft" | "review" | "ready";

export interface StepState {
  node: string;
  status: StepStatus;
  owner?: UserRef;
  updated_at: string;
}

export interface Collab {
  threads: Thread[];
  steps: StepState[];
  /** Who can be mentioned and given a step: the adventure's authors. */
  people: UserRef[];
  reactions: Reaction[];
}

export const getCollab = (slug: string, signal?: AbortSignal) => get<Collab>(`${adventure(slug)}/collab`, signal ? { signal } : {});

export const startThread = (slug: string, node: string, body: string, blockId = "") =>
  post<{ thread_id: number; comment_id: number }>(`${adventure(slug)}/threads`, { node, block_id: blockId, body });

/** Propose a change to a step, as a thread its authors accept or reject. */
export const suggestChange = (slug: string, node: string, base: unknown, proposed: unknown, note: string) =>
  post<{ thread_id: number; comment_id: number }>(`${adventure(slug)}/threads`, { node, body: note, suggestion: { base, proposed } });

/** Record a suggestion accepted (after merging it into the draft) or rejected; it resolves the thread. */
export const decideSuggestion = (slug: string, threadId: number, state: "accepted" | "rejected") =>
  patch<void>(`${adventure(slug)}/threads/${threadId}`, { suggestion_state: state });

/** Add the caller's emoji to a comment, or take it back. */
export const toggleReaction = (slug: string, target: string, emoji: string) => post<{ on: boolean }>(`${adventure(slug)}/reactions`, { target, emoji });

export const replyToThread = (slug: string, threadId: number, body: string) =>
  post<{ thread_id: number; comment_id: number }>(`${adventure(slug)}/threads/${threadId}/comments`, { body });

export const resolveThread = (slug: string, threadId: number, resolved: boolean) =>
  patch<void>(`${adventure(slug)}/threads/${threadId}`, { resolved });

export const editComment = (slug: string, commentId: number, body: string) =>
  patch<void>(`${adventure(slug)}/comments/${commentId}`, { body });

export const deleteComment = (slug: string, commentId: number) =>
  request<void>(`${adventure(slug)}/comments/${commentId}`, { method: "DELETE" });

/** Fields left out stay as they are; owner 0 gives the step to nobody. */
export const setStepStatus = (slug: string, node: string, change: { status?: StepStatus; owner_id?: number }) =>
  put<void>(`${adventure(slug)}/steps/${encodeURIComponent(node)}/status`, change);

/** The draft as the caller last saw it; null before their first visit. */
export const getSeen = <G>(slug: string, signal?: AbortSignal) =>
  get<{ seen_at: string; graph: G } | undefined>(`${adventure(slug)}/seen`, signal ? { signal } : {}).then((r) => r ?? null);

/** The caller has now seen the draft as it is stored. */
export const markSeen = (slug: string, keepalive = false) => request<void>(`${adventure(slug)}/seen`, { method: "POST", keepalive });
