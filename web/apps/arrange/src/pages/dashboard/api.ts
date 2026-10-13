import { get, post } from "@zonventure/api";

/** internal/admin/organizations.go: organizationDTO (the fields read here). */
export interface OrgRow {
  id: number;
  name: string;
  members: { id: number }[] | null;
  players_total: number;
  events_total: number;
  events_running: number;
  has_logo: boolean;
  is_publisher: boolean;
}

/** internal/admin/catalog.go: ListCatalog's body. */
export interface Catalog {
  publisher_org_id: number | null;
  adventures: { slug: string; published: boolean }[];
}

/** internal/admin/purchases.go: purchaseDTO (the fields read here). */
export interface PurchaseRow {
  id: number;
  org_id: number;
  org_name?: string;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  amount_cents: number;
  currency: string;
  provider: string;
  state: string;
  created_at: string;
  paid_at?: string;
  billed_at?: string;
  voided_at?: string;
}

const opts = (signal?: AbortSignal) => (signal ? { signal } : {});

export const listOrganizations = (signal?: AbortSignal) =>
  get<{ organizations: OrgRow[] }>("/api/admin/organizations", opts(signal)).then((r) => r.organizations ?? []);

/** Off-platform invoice purchases the platform admin has yet to bill, as Settings → Purchases' "To bill". */
export const listToInvoice = (signal?: AbortSignal) =>
  get<{ purchases: PurchaseRow[] | null }>("/api/admin/purchases", opts(signal)).then((r) =>
    (r.purchases ?? []).filter((p) => p.provider === "invoice" && p.state === "paid" && !p.billed_at && !p.voided_at),
  );

export const getCatalog = (signal?: AbortSignal) => get<Catalog>("/api/admin/catalog", opts(signal));

/** The platform admin's: a new tenant. */
export const createOrganization = (name: string) => post<{ id: number } | undefined>("/api/admin/organizations", { name });

/** A user's own: they become its org_admin and the session moves to it. */
export const createOwnOrganization = (name: string) => post<unknown>("/api/admin/me/organizations", { name });
