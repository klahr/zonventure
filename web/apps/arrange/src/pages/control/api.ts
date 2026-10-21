import { get, post } from "@zonventure/api";
import type { Zone } from "@zonventure/graph";
import type { Message, Photo } from "../event/api.ts";

/** internal/admin/handlers.go: eventDTO (detail fields race control reads). */
export interface EventInfo {
  id: number;
  name?: string;
  adventure_slug: string;
  adventure_title?: string;
  state: string;
  scheduled_at?: string;
  started_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  paused_seconds?: number;
  paused_at?: string;
  live_token: string;
  allowed_ops?: string[];
  public_for_sale?: boolean;
}

/** internal/admin/ws.go: eventDetailMemberDTO. */
export interface Member {
  id: number;
  name: string;
  last_login_at?: string;
}

/** internal/admin/ws.go: eventDetailTeamDTO. */
export interface Team {
  id: number;
  name: string;
  completed_at?: string;
  withdrawn_at?: string;
  avatar_url?: string;
  members: Member[] | null;
  last_activity_at?: string;
  score?: number;
  rank?: number;
  node?: string;
  outcome?: string;
  /** Played a step with the cheat password: unranked. */
  cheated?: boolean;
  /** Where each group is while the team is split up (a split step); node is then the split. */
  groups?: TeamGroup[];
}

/** internal/admin/ws.go: teamGroupDTO. */
export interface TeamGroup {
  port: string;
  label: string;
  node: string;
  /** label's parts, one per split the group came down, outermost first. */
  labels?: string[];
  /** When the group reached its step, moved past the event's ended pauses: how long it has been on it. */
  since?: string;
  /** Waits at its join for the others of its split. */
  arrived?: boolean;
}

/** internal/admin/ws.go: eventDetailDTO — GET /events/{id} and the event_detail push. */
export interface EventDetail {
  event: EventInfo;
  teams: Team[] | null;
  server_now: string;
  graph?: { puzzle_count: number; titles: Record<string, string>; zones: Zone[] | null };
}

/** internal/tracks: Track. server_ts is "YYYY-MM-DD HH:MM:SS" UTC. */
export interface Track {
  team_id: number;
  team_name: string;
  avatar_url?: string;
  points: { lat: number; lng: number; server_ts: string }[] | null;
  /**
   * Set on the track of one group of a team split up: its key and name,
   * and the name's parts, one per split, outermost first. A team can have
   * its own track beside its groups'.
   */
  group?: string;
  group_label?: string;
  group_labels?: string[];
  /** A trail walked before (a group since met the others, or the team before it split): a line, but no marker. */
  past?: boolean;
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

const ev = (id: number) => `/api/admin/events/${id}`;

export const getDetail = (id: number, signal?: AbortSignal) => get<EventDetail>(ev(id), signal ? { signal } : {});
export const getTracks = (id: number, signal?: AbortSignal) =>
  get<{ tracks: Track[] | null }>(ev(id) + "/tracks", signal ? { signal } : {}).then((r) => r.tracks ?? []);
export const getActivity = (id: number, signal?: AbortSignal) =>
  get<{ events: Activity[] | null }>(ev(id) + "/activity", signal ? { signal } : {}).then((r) => r.events ?? []);
export const getMessages = (id: number, signal?: AbortSignal) =>
  get<{ messages: Message[] | null }>(ev(id) + "/messages", signal ? { signal } : {});
export const getPhotos = (id: number, signal?: AbortSignal) =>
  get<{ photos: Photo[] | null; challenges?: number }>(ev(id) + "/photos", signal ? { signal } : {});

export const transition = (id: number, action: "pause" | "resume") => post<void>(`${ev(id)}/${action}`);
export const cancelEvent = (id: number, password: string) => post<void>(`${ev(id)}/cancel`, undefined, { headers: { "X-Confirm-Password": password } });
export const endEvent = (id: number, password: string) => post<void>(`${ev(id)}/end`, undefined, { headers: { "X-Confirm-Password": password } });
export const withdrawTeam = (teamId: number) => post<unknown>(`/api/admin/teams/${teamId}/withdraw`);
export const unwithdrawTeam = (teamId: number) => post<unknown>(`/api/admin/teams/${teamId}/unwithdraw`);
