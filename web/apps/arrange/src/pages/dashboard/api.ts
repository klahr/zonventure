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

const opts = (signal?: AbortSignal) => (signal ? { signal } : {});

export const listOrganizations = (signal?: AbortSignal) =>
  get<{ organizations: OrgRow[] }>("/api/admin/organizations", opts(signal)).then((r) => r.organizations ?? []);

export const getCatalog = (signal?: AbortSignal) => get<Catalog>("/api/admin/catalog", opts(signal));

/** The platform admin's: a new tenant. */
export const createOrganization = (name: string) => post<unknown>("/api/admin/organizations", { name });

/** A user's own: they become its org_admin and the session moves to it. */
export const createOwnOrganization = (name: string) => post<unknown>("/api/admin/me/organizations", { name });
