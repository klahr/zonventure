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

const base = (slug: string) => `/api/admin/adventures/${encodeURIComponent(slug)}/graph`;

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

/** Autosave the draft. Errors do not block; the report lists them. */
export const saveGraph = <Env, Rep>(slug: string, envelope: Env) =>
  put<GraphPayload<Env, Rep>>(base(slug), envelope);

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

/** Build a graph from the adventure's lanes. Nothing is saved. */
export const convertGraph = <Env, Rep>(slug: string) =>
  post<GraphPayload<Env, Rep>>(base(slug) + "/convert");

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
  accent_color: string;
  accent_secondary_color: string;
  background_color: string;
  text_color: string;
  submit_bg_color: string;
}

/**
 * The adventure's palette — the colours the player paints a step with.
 * It belongs to the adventure, not to a graph version: recolouring does
 * not make a new version, and applies to events already running.
 */
export async function getLook(slug: string, signal?: AbortSignal): Promise<{ title: string; brand: Brand }> {
  const a = await get<AdventureEdit>(`/api/admin/adventures/${encodeURIComponent(slug)}/edit`, signal ? { signal } : {});
  const b: Brand = {};
  if (a.accent_color) b.accent = a.accent_color;
  if (a.accent_secondary_color) b.accent_secondary = a.accent_secondary_color;
  if (a.background_color) b.background = a.background_color;
  if (a.text_color) b.text = a.text_color;
  if (a.submit_bg_color) b.submit_bg = a.submit_bg_color;
  return { title: a.title, brand: b };
}

/** Save the palette. A missing colour is cleared back to the default. */
export const saveBrand = (slug: string, b: Brand) =>
  patch<void>(`/api/admin/adventures/${encodeURIComponent(slug)}`, {
    accent_color: b.accent ?? "",
    accent_secondary_color: b.accent_secondary ?? "",
    background_color: b.background ?? "",
    text_color: b.text ?? "",
    submit_bg_color: b.submit_bg ?? "",
  });

// --- media -----------------------------------------------------------------

/** internal/admin: mediaView */
export interface MediaUpload {
  id: number;
  kind: "image" | "audio";
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
  return request<MediaUpload>(`/api/admin/adventures/${encodeURIComponent(slug)}/media`, { method: "POST", form });
}
