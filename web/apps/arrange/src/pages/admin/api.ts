import { get, post, put, patch, request } from "@zonventure/api";

/** internal/admin/organizations.go orgMemberDTO */
export interface OrgMember {
  id: number;
  username: string;
  role: "org_admin" | "org_member";
  email_verified: boolean;
  active_events_owned: number;
}

/** internal/admin/organizations.go assignmentDTO: an event pass. */
export interface Assignment {
  id: number;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  created_at: string;
  event_id?: number;
}

/** internal/admin/organizations.go organizationDTO */
export interface Organization {
  id: number;
  name: string;
  slug?: string;
  created_at: string;
  members: OrgMember[];
  assignments: Assignment[];
  players_total: number;
  events_total: number;
  events_running: number;
  has_logo: boolean;
  trusted: boolean;
  is_publisher: boolean;
  party_adventures: boolean;
}

/** internal/admin/handlers.go eventDTO, the fields the operator view reads. */
export interface OrgEvent {
  id: number;
  name?: string;
  adventure_slug: string;
  adventure_title?: string;
  state: string;
  scheduled_at?: string;
  started_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  assignment_id: number;
  max_players: number;
  player_slots_count: number;
  public_for_sale: boolean;
}

/** internal/admin/authoring.go shareView */
export interface Share {
  org_id: number;
  org_name: string;
  granted_at?: string;
}

/** internal/admin/catalog.go catalogAdventureDTO */
export interface CatalogAdventure {
  slug: string;
  title: string;
  language: string;
  published: boolean;
  playable: boolean;
  avatar_url?: string;
  shares: Share[] | null;
}

/** internal/admin/purchases.go purchaseDTO */
export interface Purchase {
  id: number;
  org_id: number;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  amount_cents: number;
  currency: string;
  provider: string;
  state: string;
  invoice_url?: string;
  created_at: string;
  paid_at?: string;
  billed_at?: string;
  voided_at?: string;
  pre_migration?: boolean;
}

/** internal/admin/stripe_connect.go stripeConnectStatusDTO */
export interface ConnectStatus {
  account_id?: string;
  charges_enabled: boolean;
  payouts_enabled: boolean;
  synced_at?: string;
  platform_fee_enabled: boolean;
}

const pw = (password: string) => ({ headers: { "X-Confirm-Password": password } });
const org = (id: number) => `/api/admin/organizations/${id}`;
const slug = (s: string) => encodeURIComponent(s);

export const listOrganizations = (signal?: AbortSignal) =>
  get<{ organizations: Organization[] }>("/api/admin/organizations", signal ? { signal } : {}).then((r) => r.organizations ?? []);
export const getOrganization = (id: number, signal?: AbortSignal) => get<Organization>(org(id), signal ? { signal } : {});
export const listOrgEvents = (id: number, signal?: AbortSignal) =>
  get<{ events: OrgEvent[] }>(`${org(id)}/events`, signal ? { signal } : {}).then((r) => r.events ?? []);
export const listCatalog = (signal?: AbortSignal) =>
  get<{ adventures: CatalogAdventure[] }>("/api/admin/catalog", signal ? { signal } : {}).then((r) => r.adventures ?? []);
export const listPurchases = (signal?: AbortSignal) =>
  get<{ purchases: Purchase[] }>("/api/admin/purchases", signal ? { signal } : {}).then((r) => r.purchases ?? []);
export const getPurchase = (id: number) => get<Purchase>(`/api/admin/purchases/${id}`);
export const connectStatus = (id: number, signal?: AbortSignal) =>
  get<ConnectStatus>(`${org(id)}/stripe-connect/status`, signal ? { signal } : {});
export const refreshConnect = (id: number) => post<ConnectStatus>(`${org(id)}/stripe-connect/refresh`);

export const createOrganization = (name: string) => post<Organization>("/api/admin/organizations", { name });
export const renameOrganization = (id: number, name: string) => patch<void>(org(id), { name });
export const setTrusted = (id: number, trusted: boolean) => post<unknown>(`${org(id)}/trusted`, { trusted });
export const setPartyAdventures = (id: number, enabled: boolean) => post<unknown>(`${org(id)}/party-adventures`, { enabled });
export const setPublisher = (id: number, isPublisher: boolean, password?: string) =>
  put<void>(`${org(id)}/publisher`, { is_publisher: isPublisher }, password ? pw(password) : {});
export const deleteOrganization = (id: number, password: string) => request<void>(org(id), { method: "DELETE", ...pw(password) });

export function uploadLogo(id: number, file: File) {
  const form = new FormData();
  form.append("file", file);
  return request<unknown>(`${org(id)}/logo`, { method: "POST", form });
}
export const deleteLogo = (id: number) => request<void>(`${org(id)}/logo`, { method: "DELETE" });

export const searchUsers = (q: string, orgId: number) =>
  get<{ users: { id: number; username: string }[] }>(`/api/admin/users/search?q=${encodeURIComponent(q)}&org_id=${orgId}`).then((r) => r.users ?? []);
export const addMember = (id: number, body: { username: string; role: string; password?: string; email?: string }) =>
  post<unknown>(`${org(id)}/members`, body);
export const removeMember = (id: number, userId: number, password: string) =>
  request<void>(`${org(id)}/members/${userId}`, { method: "DELETE", ...pw(password) });
export const verifyEmail = (userId: number) => post<void>(`/api/admin/users/${userId}/verify-email`);

export const createAssignment = (id: number, adventureSlug: string, maxPlayers: number) =>
  post<Assignment>(`${org(id)}/assignments`, { adventure_slug: adventureSlug, max_players: maxPlayers });
export const updateAssignment = (id: number, assignmentId: number, body: { max_players?: number; adventure_slug?: string }) =>
  patch<Assignment>(`${org(id)}/assignments/${assignmentId}`, body);
export const deleteAssignment = (id: number, assignmentId: number) =>
  request<void>(`${org(id)}/assignments/${assignmentId}`, { method: "DELETE" });

export const grantSlots = (eventId: number, addedPlayers: number, amountCents: number) =>
  post<unknown>(`/api/admin/events/${eventId}/grant-slots`, { added_players: addedPlayers, amount_cents: amountCents });
export const cancelOrgEvent = (id: number, eventId: number, password: string) =>
  post<void>(`${org(id)}/events/${eventId}/cancel`, undefined, pw(password));

export const setPublished = (s: string, published: boolean) => put<void>(`/api/admin/catalog/${slug(s)}/published`, { published });
export const grantShare = (s: string, orgId: number) => post<void>(`/api/admin/catalog/${slug(s)}/shares`, { org_id: orgId });
export const revokeShare = (s: string, orgId: number) => request<void>(`/api/admin/catalog/${slug(s)}/shares/${orgId}`, { method: "DELETE" });

export const setBilled = (purchaseId: number, billed: boolean) => post<unknown>(`/api/admin/purchases/${purchaseId}/billed`, { billed });
export const voidPurchase = (purchaseId: number) => post<unknown>(`/api/admin/purchases/${purchaseId}/void`);
export const refundPurchase = (purchaseId: number) => post<unknown>(`/api/admin/purchases/${purchaseId}/refund`);

/** internal/logx/logx.go Entry.MarshalJSON */
export interface LogEntry {
  seq: number;
  ts: string;
  level: string;
  msg: string;
  attrs?: { k: string; v: unknown }[];
}

export const listLogs = (signal?: AbortSignal, since = 0) =>
  get<{ level: string; entries: LogEntry[] | null }>(since > 0 ? `/api/admin/logs?since=${since}` : "/api/admin/logs", signal ? { signal } : {});
export const setLogLevel = (level: string) => put<{ level: string }>("/api/admin/logs/level", { level });
