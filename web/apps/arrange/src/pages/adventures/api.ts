import { get, patch, post, put, request, type Membership, type Whoami } from "@zonventure/api";
import { outline, type Graph } from "@zonventure/graph";

const adv = (slug: string) => `/api/admin/adventures/${encodeURIComponent(slug)}`;
const opt = (signal?: AbortSignal) => (signal ? { signal } : {});

/** internal/admin/handlers.go: adventureDTO */
export interface AdventureRow {
  slug: string;
  title: string;
  description?: string;
  author?: string;
  page_count: number;
  org_id?: number;
  mine_to_edit: boolean;
  avatar_url?: string;
  /** The look's starter theme and own cover picture (a media id), for a missing avatar. */
  theme?: string;
  cover?: number;
  published: boolean;
  shared: boolean;
  language: string;
  playable: boolean;
}

/** internal/admin/organizations.go: assignmentDTO — an event pass. */
export interface Assignment {
  id: number;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  created_at: string;
  event_id?: number;
}

/** internal/graph: Zone */
export interface Zone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
  polygon?: [number, number][];
}

/** internal/admin/users.go: graphSummary */
export interface GraphSummary {
  steps: { slug: string; type: string; title?: string; zone?: string; pictures?: number; sounds?: number; hints?: number }[];
  puzzle_count: number;
  story_count: number;
  zones: Zone[];
  has_quiz?: boolean;
}

/** internal/admin/users.go: AdventureDetail (a map, not a struct). */
export interface AdventureDetail {
  slug: string;
  title: string;
  description?: string;
  author?: string;
  intro_text?: string;
  outro_text?: string;
  intro_html?: string;
  outro_html?: string;
  colors?: { accent?: string };
  avatar_url?: string;
  theme?: string;
  cover?: number;
  published: boolean;
  running_event_count?: number;
  language?: string;
  org_id?: number;
  mine_to_edit: boolean;
  /** A quiz trail, edited in the simple creator. */
  trail?: boolean;
  start_lat?: number;
  start_lng?: number;
  start_label?: string;
  graph?: GraphSummary;
}

/** internal/admin/authoring.go: adventureEditDTO, the fields the details dialog edits. */
export interface AdventureEdit {
  slug: string;
  title: string;
  description: string;
  author: string;
  intro_text: string;
  outro_text: string;
  language: string;
  avatar_url?: string;
  strict_assets: boolean;
  /** Unlocks cheating on the phone, for testers; empty is none. */
  cheat_password?: string;
  start_lat?: number;
  start_lng?: number;
  start_label?: string;
  /** Set when teams can only begin within this many metres of the start. */
  start_radius_m?: number;
}

export type AdventurePatch = Partial<Omit<AdventureEdit, "avatar_url" | "start_lat" | "start_lng" | "start_radius_m">> & {
  start_lat?: number | null;
  start_lng?: number | null;
  start_radius_m?: number | null;
  /** Replaces the whole brief: what the adventure is meant to be. */
  brief?: Brief;
};

/** internal/admin: Brief — free text, as agreed with the author. */
export interface Brief {
  theme?: string;
  setting?: string;
  tone?: string;
  audience?: string;
  age_range?: string;
  group_size?: string;
  minutes?: number;
  difficulty?: string;
  distance_m?: number;
  accessibility?: string;
  premise?: string;
  notes?: string;
}

export const listAdventures = (signal?: AbortSignal) =>
  get<{ adventures: AdventureRow[] }>("/api/admin/adventures", opt(signal)).then((r) => r.adventures ?? []);

export const listAssignments = (signal?: AbortSignal) =>
  get<{ assignments: Assignment[] }>("/api/admin/assignments", opt(signal)).then((r) => r.assignments ?? []);

export const getAdventure = (slug: string, signal?: AbortSignal) => get<AdventureDetail>(adv(slug), opt(signal));

export const getEdit = (slug: string, signal?: AbortSignal) => get<AdventureEdit>(adv(slug) + "/edit", opt(signal));

/** The draft graph's nodes and what is published, for an adventure made in the designer. */
export const getGraphState = (slug: string, signal?: AbortSignal) =>
  get<{ envelope?: { graph?: Graph }; published?: { version: number } }>(adv(slug) + "/graph", opt(signal));

/**
 * The first place a team reaches: the zone of the first step, in the
 * order it is played, that is gated on one; else the first zone. For a
 * start point to copy, from the published summary or the draft.
 */
export function firstPlaceOf(g: GraphSummary | Graph | null | undefined): { lat: number; lng: number } | null {
  if (!g) return null;
  const zones = g.zones ?? [];
  const at = (slug: string | undefined) => zones.find((z) => z.slug === slug);
  const zoneOfStep =
    "steps" in g
      ? g.steps.map((s) => s.zone)
      : outline(g).flatMap((r) => {
          if (r.kind !== "step") return [];
          const n = g.nodes.find((x) => x.slug === r.slug);
          return [n?.puzzle?.zone ?? n?.story?.zone ?? n?.start?.zone];
        });
  const z = zoneOfStep.map(at).find(Boolean) ?? zones[0];
  return z ? { lat: z.lat, lng: z.lng } : null;
}

/** internal/admin/authoring.go: CreateAdventure */
export const createAdventure = (body: { slug: string; title: string; description: string; author: string }) =>
  post<{ slug: string }>("/api/admin/adventures", body);

export const updateAdventure = (slug: string, body: AdventurePatch) => patch<void>(adv(slug), body);

export const deleteAdventure = (slug: string, password: string) =>
  request<void>(adv(slug), { method: "DELETE", headers: { "X-Confirm-Password": password } });

/** internal/admin/duplicate.go: a copy in the caller's org, "<title> (copy)" by default. */
export const duplicateAdventure = (slug: string) => post<{ slug: string; title?: string }>(adv(slug) + "/duplicate", {});

export const exportURL = (slug: string) => adv(slug) + "/export";

/** internal/admin/portability.go: ImportAdventure. 409 when the slug is taken; ?slug= renames. */
export function importAdventure(file: File, slug?: string) {
  const form = new FormData();
  form.append("file", file);
  return request<{ slug: string; title?: string }>("/api/admin/adventures/import" + (slug ? `?slug=${encodeURIComponent(slug)}` : ""), { method: "POST", form });
}

/** internal/admin/avatars.go */
export function uploadCover(slug: string, file: Blob) {
  const form = new FormData();
  form.append("file", file);
  return request<{ url: string }>(adv(slug) + "/avatar", { method: "POST", form });
}

export const removeCover = (slug: string) => request<void>(adv(slug) + "/avatar", { method: "DELETE" });

type PublisherMembership = Membership & { is_publisher?: boolean };

/** The org the user works in authors the platform catalog (internal/auth/handlers.go: membershipDTO.is_publisher). */
export function isPublisher(user: Whoami | null): boolean {
  const m = user?.memberships.find((x) => x.org_id === user.org_id) as PublisherMembership | undefined;
  return !!m?.is_publisher;
}

export function orgName(user: Whoami | null): string {
  return user?.memberships.find((x) => x.org_id === user.org_id)?.org_name ?? "";
}

/**
 * Shrink a large photo before upload: phones take 4000px pictures and a
 * cover is shown at 96px. GIF and SVG pass through untouched.
 */
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
    return blob && blob.size < file.size ? new File([blob], file.name.replace(/\.[^.]+$/, "") + ".jpg", { type: "image/jpeg" }) : file;
  } finally {
    bitmap.close();
  }
}

/** internal/admin/position.go: the author's phone reports where it stands. */
export const putMyPosition = (p: { lat: number; lng: number; accuracy_m: number }) =>
  put<void>("/api/admin/me/position", p);
