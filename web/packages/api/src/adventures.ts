// Adventure and graph endpoints. Routes: internal/server/routes_admin.go;
// handlers: internal/admin/handlers.go (list) and internal/admin/graph.go.

import { ApiError, get, post, put } from "./http.ts";

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

export const saveGraph = <Env, Rep>(slug: string, envelope: Env) =>
  put<GraphPayload<Env, Rep>>(base(slug), envelope);

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
