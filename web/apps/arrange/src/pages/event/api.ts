import { get, patch, post, request } from "@zonventure/api";

/** internal/admin/handlers.go: eventDTO (detail payload). */
export interface EventInfo {
  id: number;
  name?: string;
  adventure_slug: string;
  adventure_title?: string;
  state: "scheduled" | "running" | "paused" | "completed" | "cancelled" | string;
  scheduled_at?: string;
  started_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  paused_seconds?: number;
  paused_at?: string;
  org_id: number;
  assignment_id: number;
  max_players: number;
  live_token: string;
  show_map: boolean;
  teams_count: number;
  player_slots_count: number;
  allowed_ops?: string[];
  public_for_sale: boolean;
  ticket_price_cents?: number;
  ticket_currency?: string;
  ticket_team_size?: number;
  public_blurb?: string;
  has_cover_image?: boolean;
}

/** internal/admin/ws.go: eventDetailMemberDTO. */
export interface Member {
  id: number;
  name: string;
  token: string;
  last_login_at?: string;
  expires_at?: string;
}

/** internal/admin/ws.go: eventDetailTeamDTO. */
export interface Team {
  id: number;
  name: string;
  email?: string;
  completed_at?: string;
  withdrawn_at?: string;
  /** Used the adventure's cheat password: no rank. */
  cheated?: boolean;
  token: string;
  recap_token?: string;
  avatar_url?: string;
  members: Member[] | null;
  last_activity_at?: string;
  score?: number;
  rank?: number;
  node?: string;
  outcome?: string;
}

/** internal/admin/ws.go: eventGraphDTO (zones left out: this page has no map). */
export interface EventGraph {
  puzzle_count: number;
  titles: Record<string, string>;
}

/** internal/admin/ws.go: eventDetailDTO — GET /events/{id} and the watch push alike. */
export interface EventDetail {
  event: EventInfo;
  teams: Team[] | null;
  server_now?: string;
  graph?: EventGraph | null;
}

/** internal/admin/events.go: activityDTO. */
export interface Activity {
  id: number;
  team_id?: number;
  team_name?: string;
  actor?: string;
  kind: string;
  payload: Record<string, unknown> | null;
  created_at: string;
}

/** internal/admin/users.go: AdventureDetail (the fields this page reads). */
export interface Adventure {
  slug: string;
  title?: string;
  author?: string;
  language?: string;
  avatar_url?: string;
}

/** internal/eventmsg/eventmsg.go: Message. */
export interface Message {
  id: number;
  text: string;
  team_ids?: number[];
  created_at: string;
}

/** internal/admin/photos.go: TeamPhoto. */
export interface Photo {
  id: number;
  team_id: number;
  team_name: string;
  node: string;
  node_title?: string;
  block: string;
  prompt?: string;
  worth: number;
  status: "pending" | "approved" | "denied" | string;
  created_at: string;
  reviewed_at?: string;
}

/** The Stripe Connect status endpoint (internal/admin/stripe_connect.go). */
export interface ConnectStatus {
  platform_fee_enabled?: boolean;
  charges_enabled?: boolean;
}

/** One team of a roster, as /teams/bulk takes it and /teams/import returns it. */
export interface RosterTeam {
  name: string;
  member_names: string[];
  email?: string;
}

/** internal/admin/handlers.go: BulkCreateTeams response. */
export interface BulkResult {
  created?: number;
  failed?: number;
  results?: { error?: string }[];
}

/** internal/admin/statistics.go: EventStats — anonymous, across every team that played. */
export interface EventStats {
  teams: number;
  finished: number;
  failed: number;
  guesses: number;
  wrong: number;
  first_try: number;
  solved: number;
  hints: number;
  photos: number;
  median_seconds?: number;
  fastest_seconds?: number;
  slowest_seconds?: number;
  median_score?: number;
  top_score?: number;
  avg_distance_m?: number;
  puzzles: PuzzleStats[];
}

export interface PuzzleStats {
  node: string;
  title: string;
  reached: number;
  solved: number;
  first_try: number;
  wrong: number;
  hints: number;
  median_seconds?: number;
  wrong_answers: { answer: string; count: number; teams: number }[];
}

const E = (id: number) => `/api/admin/events/${id}`;
const pw = (password: string) => ({ headers: { "X-Confirm-Password": password } });

export const getDetail = (id: number, signal?: AbortSignal) => get<EventDetail>(E(id), signal ? { signal } : {});
export const getActivity = (id: number, teamId?: number, signal?: AbortSignal) =>
  get<{ events: Activity[] | null }>(E(id) + "/activity" + (teamId ? `?team_id=${teamId}` : ""), signal ? { signal } : {}).then((r) => r.events ?? []);
export const getAdventure = (slug: string, signal?: AbortSignal) => get<Adventure>(`/api/admin/adventures/${encodeURIComponent(slug)}`, signal ? { signal } : {});
export const getConnectStatus = (orgId: number, signal?: AbortSignal) =>
  get<ConnectStatus>(`/api/admin/organizations/${orgId}/stripe-connect/status`, signal ? { signal } : {});
export const getMessages = (id: number, signal?: AbortSignal) =>
  get<{ messages: Message[] | null }>(E(id) + "/messages", signal ? { signal } : {});
export const getPhotos = (id: number, signal?: AbortSignal) =>
  get<{ photos: Photo[] | null; challenges?: number }>(E(id) + "/photos", signal ? { signal } : {});
export const getStatistics = (id: number, signal?: AbortSignal) => get<EventStats>(E(id) + "/statistics", signal ? { signal } : {});

export const transition = (id: number, action: "start" | "pause" | "resume") => post<void>(`${E(id)}/${action}`);
export const cancelEvent = (id: number, password: string) => post<void>(E(id) + "/cancel", undefined, pw(password));
export const patchEvent = (id: number, body: Record<string, unknown>) => patch<void>(E(id), body);
export const sendInvitations = (id: number) =>
  post<{ sent?: number; skipped_no_email?: number; failed?: number }>(E(id) + "/invitations/send", {});
export const uploadCover = (id: number, file: File) => {
  const form = new FormData();
  form.append("file", file);
  return request<void>(E(id) + "/cover", { method: "POST", form });
};
export const deleteCover = (id: number) => request<void>(E(id) + "/cover", { method: "DELETE" });
export const topUp = (body: { adventure_slug: string; max_players: number; top_up_assignment_id: number; provider?: string }) =>
  post<{ state: string; checkout_url?: string }>("/api/admin/purchases", body);

export const sendMessage = (id: number, text: string, teamIds?: number[]) =>
  post<void>(E(id) + "/messages", { text, ...(teamIds ? { team_ids: teamIds } : {}) });
export const deleteMessage = (id: number, messageId: number) => request<void>(`${E(id)}/messages/${messageId}`, { method: "DELETE" });
export const photoSrc = (id: number, photoId: number) => `${E(id)}/photos/${photoId}/image`;
export const reviewPhoto = (id: number, photoId: number, status: string) => post<void>(`${E(id)}/photos/${photoId}/review`, { status });

const T = (id: number) => `/api/admin/teams/${id}`;
export const createTeam = (eventId: number, body: RosterTeam) => post<unknown>(E(eventId) + "/teams", body);
export const bulkTeams = (eventId: number, teams: RosterTeam[]) => post<BulkResult>(E(eventId) + "/teams/bulk", { teams });
export const importTeams = (eventId: number, file: File) => {
  const form = new FormData();
  form.append("file", file);
  return request<{ format: string; teams: RosterTeam[] | null }>(E(eventId) + "/teams/import", { method: "POST", form });
};
export const patchTeam = (id: number, body: { name?: string; email?: string }) => patch<void>(T(id), body);
export const deleteTeam = (id: number, password: string) => request<void>(T(id), { method: "DELETE", ...pw(password) });
export const unlockTeam = (id: number, node?: string) => post<{ completed?: boolean }>(T(id) + "/unlock", node ? { node } : undefined);
export const withdrawTeam = (id: number) => post<unknown>(T(id) + "/withdraw");
export const unwithdrawTeam = (id: number) => post<unknown>(T(id) + "/unwithdraw");
export const sendTeamInvitation = (id: number) => post<unknown>(T(id) + "/invitations/send", {});
export const addSlot = (teamId: number, name: string) => post<{ name: string }>(T(teamId) + "/slots", { name });
export const rotateSlot = (teamId: number, slotId: number) => post<unknown>(`${T(teamId)}/slots/${slotId}/rotate`);
export const removeSlot = (teamId: number, slotId: number) => request<void>(`${T(teamId)}/slots/${slotId}`, { method: "DELETE" });
export const uploadTeamAvatar = (teamId: number, file: Blob, name: string) => {
  const form = new FormData();
  form.append("file", file, name);
  return request<{ url?: string }>(T(teamId) + "/avatar", { method: "POST", form });
};
export const deleteTeamAvatar = (teamId: number) => request<void>(T(teamId) + "/avatar", { method: "DELETE" });
