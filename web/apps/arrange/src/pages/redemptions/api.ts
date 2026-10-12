import { ApiError, get, patch, post, request } from "@zonventure/api";

/** internal/admin/redemptions.go partnerProductDTO */
export interface PartnerProduct {
  id: number;
  name: string;
  event_id: number;
  event_name: string;
  adventure_title: string;
  team_size: number;
  enabled: boolean;
  /** The posted letter's choices, as on the redeem page; null when its event posts nothing. */
  letter: { languages: string[]; countries: string[] } | null;
}

/** internal/admin/redemptions.go partnerDTO */
export interface Partner {
  id: number;
  kind: string;
  name: string;
  enabled: boolean;
  products: PartnerProduct[] | null;
}

/** internal/admin/redemptions.go redemptionDTO: a gift box redemption, or a store purchase whose letter is posted. */
export interface Redemption {
  kind: "redemption" | "purchase";
  id: number;
  /** A purchase is "paid". */
  state: "pending" | "approved" | "rejected" | "cancelled" | "paid";
  partner_kind: string;
  partner_name: string;
  product_id: number;
  product_name: string;
  event_id: number;
  event_name: string;
  box_number: string;
  security_code: string;
  name: string;
  email: string;
  street_address: string;
  address_2: string;
  postal_code: string;
  city: string;
  country: string;
  letter_lang: string;
  post_at: string;
  posted_at: string;
  note: string;
  created_at: string;
  decided_at: string;
  decided_by: string;
  team_id?: number;
  team_name: string;
  letter_languages: string[] | null;
  /** What to print for it, in its envelope's language. */
  print: PrintLink[] | null;
}

/** internal/admin/redemptions.go printLink */
export interface PrintLink {
  id: string;
  name: string;
  lang: string;
  media_id: number;
}

export type RedemptionState = Redemption["state"];

/** The fields of internal/admin/handlers.go eventDTO the product picker reads. */
export interface SelfGuidedEvent {
  id: number;
  name?: string;
  adventure_title?: string;
  state: string;
  ticket_team_size?: number;
}

export interface NewRedemption {
  product_id: number;
  box_number: string;
  security_code: string;
  name: string;
  email: string;
  street_address: string;
  address_2: string;
  postal_code: string;
  city: string;
  country: string;
  letter_lang: string;
  post_at: string;
  note: string;
}

export interface ProductInput {
  name: string;
  event_id: number;
  team_size: number;
}

const opt = (signal?: AbortSignal) => (signal ? { signal } : {});

export const listRedemptions = (state: string, signal?: AbortSignal) =>
  get<{ redemptions: Redemption[] | null; counts: Partial<Record<RedemptionState | "to_post", number>> }>(`/api/admin/redemptions?state=${encodeURIComponent(state)}`, opt(signal));
export const listPartners = (signal?: AbortSignal) =>
  get<{ partners: Partner[] | null }>("/api/admin/partners", opt(signal)).then((r) => r.partners ?? []);
/** internal/admin/org_profile.go: only the slug, for the redeem page link. */
export const orgSlug = (signal?: AbortSignal) => get<{ slug: string }>("/api/admin/orgs/me/profile", opt(signal)).then((r) => r.slug || "");
export const listSelfGuided = () =>
  get<{ events: SelfGuidedEvent[] | null }>("/api/admin/events?public_for_sale=true").then((r) => r.events ?? []);

export const decide = (id: number, action: "approve" | "reject" | "cancel") => post<unknown>(`/api/admin/redemptions/${id}/${action}`);
export const updateRedemption = (id: number, body: { posted?: boolean; post_at?: string; note?: string; letter_lang?: string }) => patch<unknown>(`/api/admin/redemptions/${id}`, body);
export const updatePurchase = (id: number, body: { posted?: boolean; post_at?: string; note?: string; letter_lang?: string }) => patch<unknown>(`/api/admin/ticket-purchases/${id}`, body);
export const createRedemption = (body: NewRedemption) => post<unknown>("/api/admin/redemptions", body);

export const addPartner = (kind: string) => post<unknown>("/api/admin/partners", { kind });
export const setPartnerEnabled = (id: number, enabled: boolean) => patch<unknown>(`/api/admin/partners/${id}`, { enabled });
export const deletePartner = (id: number) => request<void>(`/api/admin/partners/${id}`, { method: "DELETE" });
export const addProduct = (partnerId: number, body: ProductInput) => post<unknown>(`/api/admin/partners/${partnerId}/products`, body);
export const updateProduct = (id: number, body: Partial<ProductInput> & { enabled?: boolean }) => patch<unknown>(`/api/admin/partner-products/${id}`, body);
export const deleteProduct = (id: number) => request<void>(`/api/admin/partner-products/${id}`, { method: "DELETE" });

/** The reason code the redemption handlers put next to their error (redeemError). */
export function reasonOf(e: unknown): string | undefined {
  if (!(e instanceof ApiError)) return undefined;
  const b = e.body as { reason?: unknown } | undefined;
  return typeof b?.reason === "string" ? b.reason : undefined;
}
