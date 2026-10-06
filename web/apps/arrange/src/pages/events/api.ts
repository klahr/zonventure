import { get, post, patch, request, type Whoami } from "@zonventure/api";

/** internal/admin/handlers.go: eventDTO (list rows). */
export interface EventRow {
  id: number;
  name?: string;
  adventure_slug: string;
  adventure_title?: string;
  adventure_avatar_url?: string;
  state: "scheduled" | "running" | "paused" | "completed" | "cancelled" | string;
  scheduled_at?: string;
  started_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  assignment_id: number;
  max_players: number;
  teams_count: number;
  player_slots_count: number;
  public_for_sale: boolean;
  ticket_price_cents?: number;
  ticket_currency?: string;
  /** A hosted event, run by its host from the host page. */
  party?: boolean;
}

/** internal/admin/handlers.go: adventureDTO. */
export interface AdventureRow {
  slug: string;
  title: string;
  description?: string;
  mine_to_edit: boolean;
  avatar_url?: string;
  published: boolean;
  shared: boolean;
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

/** internal/admin/purchases.go: CreatePurchase's 201 body. */
export interface PurchaseCreated {
  id: number;
  amount_cents: number;
  currency: string;
  state: "paid" | "pending" | string;
  provider: string;
  assignment_id?: number;
  checkout_url?: string;
}

/** internal/admin/purchases.go: purchaseDTO (the fields read here). */
export interface Purchase {
  id: number;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  state: string;
  assignment_id?: number;
}

const opts = (signal?: AbortSignal) => (signal ? { signal } : {});

/**
 * publicForSale: false = events, true = self-guided, undefined = both;
 * party the same for hosted events.
 */
export const listEvents = (publicForSale?: boolean, signal?: AbortSignal, party?: boolean) => {
  const q = new URLSearchParams();
  if (publicForSale !== undefined) q.set("public_for_sale", String(publicForSale));
  if (party !== undefined) q.set("party", String(party));
  return get<{ events: EventRow[] }>("/api/admin/events" + (q.size ? `?${q}` : ""), opts(signal)).then((r) => r.events ?? []);
};

export const listAdventures = (signal?: AbortSignal) =>
  get<{ adventures: AdventureRow[] }>("/api/admin/adventures", opts(signal)).then((r) => r.adventures ?? []);

export const listAssignments = (signal?: AbortSignal) =>
  get<{ assignments: Assignment[] }>("/api/admin/assignments", opts(signal)).then((r) => r.assignments ?? []);

export interface CreateEventBody {
  name: string;
  attest_no_minors: true;
  assignment_id?: number;
  adventure_slug?: string;
  scheduled_at?: string;
  public_for_sale?: boolean;
}
export const createEvent = (body: CreateEventBody) => post<{ id: number }>("/api/admin/events", body);

export const createPurchase = (body: { adventure_slug: string; max_players: number; provider?: string }) =>
  post<PurchaseCreated>("/api/admin/purchases", body);

export const getPurchase = (id: number, signal?: AbortSignal) => get<Purchase>(`/api/admin/purchases/${id}`, opts(signal));

export const createTeam = (eventId: number, name: string, memberNames: string[]) =>
  post<unknown>(`/api/admin/events/${eventId}/teams`, { name, member_names: memberNames });

export const startEvent = (eventId: number) => post<void>(`/api/admin/events/${eventId}/start`);

export const updateEvent = (eventId: number, body: Record<string, unknown>) => patch<void>(`/api/admin/events/${eventId}`, body);

export function uploadEventCover(eventId: number, file: File) {
  const form = new FormData();
  form.append("file", file);
  return request<unknown>(`/api/admin/events/${eventId}/cover`, { method: "POST", form });
}

/** internal/auth/handlers.go: Pricing, on whoami. */
export interface Pricing {
  player_cents: number;
  free_players_per_event: number;
  currency: string;
}

/** The classic console's pricingHelpers: what a pass of n players costs. */
export function pricing(user: Whoami) {
  const p = (user.pricing ?? {}) as Partial<Pricing>;
  const priceCents = p.player_cents || 500;
  const free = p.free_players_per_event ?? 15;
  const currency = p.currency || "EUR";
  return { priceCents, free, currency, chargeable: (n: number) => Math.max(0, n - free) * priceCents };
}

/** The membership of the org the user is working in. */
export function activeMembership(user: Whoami) {
  return user.memberships.find((m) => m.org_id === user.org_id) as (Whoami["memberships"][number] & { is_publisher?: boolean }) | undefined;
}
