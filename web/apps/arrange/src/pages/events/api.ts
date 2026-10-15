import { get, post, patch, request, type Whoami } from "@zonventure/api";
import type { ListingReviewState } from "../../ui/listingStatus.tsx";

/** internal/admin/handlers.go: eventDTO (list rows). */
export interface EventRow {
  id: number;
  name?: string;
  adventure_slug: string;
  adventure_title?: string;
  adventure_avatar_url?: string;
  /** Stand-ins for a missing avatar: the look's starter theme and own cover picture. */
  adventure_theme?: string;
  adventure_cover?: number;
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
  /** A self-guided event's listing review, and whether an approved version is up. */
  listing_review?: ListingReviewState;
  listed?: boolean;
  /** What the store sells a ticket at: the approved version's price. */
  listed_price_cents?: number;
  listed_currency?: string;
  /** Why an approved listing isn't in the store: no payment provider, or no Stripe account under a platform fee. */
  store_blocked?: "no_payments" | "connect_required";
  /** Its organiser stopped selling it. */
  sales_stopped?: boolean;
}

/** internal/admin/handlers.go: adventureDTO. */
export interface AdventureRow {
  slug: string;
  title: string;
  description?: string;
  mine_to_edit: boolean;
  avatar_url?: string;
  theme?: string;
  cover?: number;
  published: boolean;
  shared: boolean;
  playable: boolean;
  language?: string;
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
  /** Not asked for a self-guided event: its players buy their own tickets. */
  attest_no_minors?: true;
  assignment_id?: number;
  adventure_slug?: string;
  scheduled_at?: string;
  public_for_sale?: boolean;
}
export const createEvent = (body: CreateEventBody) => post<{ id: number }>("/api/admin/events", body);


export const createTeam = (eventId: number, name: string, memberNames: string[]) =>
  post<unknown>(`/api/admin/events/${eventId}/teams`, { name, member_names: memberNames });

export const startEvent = (eventId: number) => post<void>(`/api/admin/events/${eventId}/start`);

export const updateEvent = (eventId: number, body: Record<string, unknown>) => patch<void>(`/api/admin/events/${eventId}`, body);

export function uploadEventCover(eventId: number, file: File) {
  const form = new FormData();
  form.append("file", file);
  return request<unknown>(`/api/admin/events/${eventId}/cover`, { method: "POST", form });
}

/** internal/auth/handlers.go: Pricing, on whoami: what a team costs. */
export interface Pricing {
  team_cents: number;
  team_phones: number;
  phone_cents: number;
  currency: string;
  /** The platform's prices are before VAT; else VAT is included. */
  vat_excluded?: boolean;
}

/** What teams cost the organisation: n teams of phones phones each. */
export function pricing(user: Whoami) {
  const p = (user.pricing ?? {}) as Partial<Pricing>;
  const team = p.team_cents ?? 4900;
  const included = p.team_phones ?? 3;
  const phone = p.phone_cents ?? 1500;
  return {
    team, included, phone, currency: p.currency, vatExcluded: !!p.vat_excluded,
    cost: (teams: number, phones: number) => teams * (team + Math.max(0, phones - included) * phone),
  };
}

/** The membership of the org the user is working in. */
export function activeMembership(user: Whoami) {
  return user.memberships.find((m) => m.org_id === user.org_id) as (Whoami["memberships"][number] & { is_publisher?: boolean }) | undefined;
}
