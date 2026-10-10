import { get, post, put, patch, request } from "@zonventure/api";

/** internal/admin/organizations.go orgMemberDTO */
/**
 * What listing review reviews (internal/admin/listing_review.go): a
 * self-guided event's store listing, a party offer, or a held-back change
 * to an organisation's page or an adventure's presentation (presentation.go).
 */
export type ListingKind = "event" | "party" | "org" | "adventure";

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
  /** Listings skip review and the store shows a badge (listing_review.go). */
  verified_creator: boolean;
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
export const setVerifiedCreator = (id: number, verified: boolean) => post<unknown>(`${org(id)}/verified-creator`, { verified });
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

/** internal/admin/listing_review.go listingFieldsDTO: what a listing shows the public. */
export interface ListingFields {
  name?: string;
  blurb: string;
  /** An event's ticket price, or a party's base price. */
  price_cents: number;
  per_kid_cents?: number;
  currency: string;
  team_size?: number;
  max_kids?: number;
  cover_url?: string;
  /** The adventure version it plays. */
  graph_id?: number;
}

/** internal/admin/listing_review.go listingReviewDTO: a listing waiting for review. */
export interface ListingReview {
  kind: ListingKind;
  id: number;
  revision: string;
  submitted_at: string;
  org_id: number;
  org_name: string;
  org_slug?: string;
  adventure_slug: string;
  adventure_title: string;
  /** Who submitted it, confirming its content follows the terms. */
  attested_by?: string;
  draft: ListingFields;
  /** What the public sees now, when the review is of an edit. */
  live?: ListingFields;
}

/** internal/admin/listing_content.go ReviewText: a text people read, and where. */
export interface ReviewText {
  where: string;
  text: string;
}

/** internal/admin/listing_content.go ReviewMedia: a picture, sound, film or file. */
export interface ReviewMedia {
  ref: string;
  kind: "image" | "audio" | "video" | "file";
  mime: string;
  where: string;
  size: number;
  url: string;
}

/** internal/admin/listing_content.go ReviewContent: everything a listing puts in front of people. */
export interface ReviewContent {
  kind: ListingKind;
  id: number;
  state: "" | "draft" | "pending" | "approved" | "rejected";
  revision: string;
  submitted_at?: string;
  attested_at?: string;
  attested_by?: string;
  reject_reason?: string;
  org_id: number;
  org_name: string;
  org_slug?: string;
  org_verified: boolean;
  adventure_slug: string;
  adventure_title: string;
  graph_id: number;
  version: number;
  /** The version approved before, 0 when none. */
  live_graph_id?: number;
  draft: ListingFields;
  live?: ListingFields;
  texts: ReviewText[];
  media: ReviewMedia[];
  /** For an organisation page or adventure presentation: what the proposal changes. */
  changes?: ReviewChange[];
}

/** internal/admin/listing_content.go ReviewChange: one field a proposal changes. */
export interface ReviewChange {
  field: string;
  live?: string;
  proposed?: string;
  live_url?: string;
  proposed_url?: string;
}

/** internal/admin/listing_review.go listingLogDTO: a submission or decision. */
export interface ListingLogEntry {
  kind: ListingKind;
  id: number;
  org_id: number;
  org_name: string;
  title: string;
  action: "submitted" | "approved" | "rejected";
  /** console, mcp (an AI assistant through an API token) or auto (a verified creator). */
  via: "console" | "mcp" | "auto";
  by?: string;
  reason?: string;
  graph_id?: number;
  created_at: string;
  /** Fingerprint of what was reviewed, and the entry's link in the hash chain. */
  content_hash: string;
  entry_hash: string;
  /** A reviewer's passkey signed the decision. */
  signed?: boolean;
}

/** internal/admin/listing_review_chain.go chainReport: the log's hash chain, recomputed. */
export interface ChainReport {
  ok: boolean;
  entries: number;
  /** Decisions signed with a reviewer's passkey, each checked again. */
  signed: number;
  /** Decisions made through an API token or approved automatically. */
  unsigned: number;
  /** Compare with the latest one mailed to platform admins. */
  head_hash: string;
  problems: { id: number; problem: string }[];
  /** What approved listings serve now, against what was approved. */
  served?: ServedReport;
}

/** internal/admin/listing_review_served.go servedReport */
export interface ServedReport {
  checked: number;
  matching: number;
  findings: ServedFinding[];
}

export interface ServedFinding {
  kind: ListingKind;
  id: number;
  org_name: string;
  title: string;
  /** changed: edited outside review. unreviewed: edited live, review doesn't cover it. unrecorded: approved before review was logged. */
  severity: "changed" | "unreviewed" | "unrecorded";
  parts?: string[];
}

/** internal/admin/listing_review.go listingReviewerDTO */
export interface ListingReviewer {
  id: number;
  username: string;
  email?: string;
  is_admin: boolean;
  /** Organisations whose listings they never see. */
  orgs: string[];
}

/** A listing a decision names: which one, and the revision reviewed. */
export type ListingRef = Pick<ListingReview, "kind" | "id" | "revision">;

const review = (r: Pick<ListingReview, "kind" | "id">) => `/api/admin/listing-reviews/${r.kind}/${r.id}`;
export const listListingReviews = (signal?: AbortSignal) =>
  get<{ reviews: ListingReview[] | null }>("/api/admin/listing-reviews", signal ? { signal } : {}).then((r) => r.reviews ?? []);
export const getListingReview = (kind: ListingKind, id: number, signal?: AbortSignal) =>
  get<ReviewContent>(review({ kind, id }), signal ? { signal } : {});
/** A decision's passkey signature, as approve and reject take it. */
export interface DecisionSignature {
  session_id: string;
  credential: Record<string, unknown>;
}
/** What the browser's passkey is asked to sign (go-webauthn's options, JSON). */
export interface PasskeyCeremony {
  session_id: string;
  options: Record<string, unknown>;
}
/** Starts signing a decision: its hash is the challenge (reviewer_passkeys.go). */
export const signListingDecision = (r: ListingRef, action: "approve" | "reject", reason: string) =>
  post<PasskeyCeremony>(`${review(r)}/sign`, { action, revision: r.revision, reason });
export const approveListing = (r: ListingRef, signature?: DecisionSignature) =>
  post<unknown>(`${review(r)}/approve`, { revision: r.revision, ...(signature ? { signature } : {}) });
export const rejectListing = (r: ListingRef, reason: string, signature?: DecisionSignature) =>
  post<unknown>(`${review(r)}/reject`, { revision: r.revision, reason, ...(signature ? { signature } : {}) });

/** internal/admin/reviewer_passkeys.go reviewerPasskeyDTO */
export interface ReviewerPasskey {
  id: number;
  name: string;
  created_at: string;
  last_used_at?: string;
}
export const listReviewerPasskeys = (signal?: AbortSignal) =>
  get<{ passkeys: ReviewerPasskey[] | null }>("/api/admin/me/reviewer-passkeys", signal ? { signal } : {}).then((r) => r.passkeys ?? []);
export const beginReviewerPasskey = () => post<PasskeyCeremony>("/api/admin/me/reviewer-passkeys/begin", {});
export const finishReviewerPasskey = (session_id: string, name: string, credential: Record<string, unknown>) =>
  post<{ id: number; name: string }>("/api/admin/me/reviewer-passkeys/finish", { session_id, name, credential });
export const revokeReviewerPasskey = (id: number) => request<void>(`/api/admin/me/reviewer-passkeys/${id}`, { method: "DELETE" });
export const listListingLog = (via: string, signal?: AbortSignal) =>
  get<{ log: ListingLogEntry[] | null }>("/api/admin/listing-reviews/log" + (via ? `?via=${encodeURIComponent(via)}` : ""), signal ? { signal } : {}).then((r) => r.log ?? []);
export const verifyListingLog = () => get<ChainReport>("/api/admin/listing-reviews/log/verify");
export const listListingReviewers = (signal?: AbortSignal) =>
  get<{ reviewers: ListingReviewer[] | null }>("/api/admin/listing-reviewers", signal ? { signal } : {}).then((r) => r.reviewers ?? []);
export const setListingReviewer = (username: string, reviewer: boolean) =>
  post<unknown>("/api/admin/listing-reviewers", { username, reviewer });
