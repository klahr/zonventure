import { get, post, put, request } from "@zonventure/api";

/** domainState, internal/admin/domains.go. */
export interface DomainState {
  host: string;
  /** What a DNS panel asks for as the record's name: "adventures". */
  name: string;
  apex: string;
  /** The CNAME's value. */
  value: string;
  status: "waiting" | "wrong" | "certificate" | "live";
  /** doubled | address | cloudflare | other_target | certificate, or "". */
  problem: string;
  seen: string;
  checked_at?: string;
  live_at?: string;
  down_since?: string;
}

/** domainDTO — GET /api/admin/orgs/me/domain. */
export interface DomainInfo {
  enabled: boolean;
  entitled: boolean;
  until?: string;
  grace_until?: string;
  in_grace: boolean;
  /** Included with no end date. */
  forever: boolean;
  contact_url: string;
  price_year_cents: number;
  currency: string;
  vat_excluded: boolean;
  suggestion: string;
  store_url: string;
  org_name: string;
  domain: DomainState | null;
  providers: { id: string; name: string }[];
}

const BASE = "/api/admin/orgs/me/domain";

export const getDomain = (signal?: AbortSignal) => get<DomainInfo>(BASE, signal ? { signal } : {});
export const putDomain = (host: string) => put<DomainInfo>(BASE, { host });
export const deleteDomain = () => request<DomainInfo>(BASE, { method: "DELETE" });
export const checkDomain = () => post<DomainInfo>(`${BASE}/check`, {});
export const getGuide = (provider: string, lang: string) =>
  get<{ steps: string }>(`${BASE}/guide/${encodeURIComponent(provider)}?lang=${encodeURIComponent(lang)}`).then((r) => r.steps);
export const sendInstructions = (email: string, provider: string, lang: string) =>
  post<{ sent_to: string }>(`${BASE}/send`, { email, provider, lang });

/** orgCustomDomainDTO — the platform admin's view. */
export interface OrgCustomDomain {
  until: string;
  domain: DomainState | null;
}
export const getOrgCustomDomain = (orgID: number, signal?: AbortSignal) =>
  get<OrgCustomDomain>(`/api/admin/organizations/${orgID}/custom-domain`, signal ? { signal } : {});
export const putOrgCustomDomain = (orgID: number, until: string) =>
  put<OrgCustomDomain>(`/api/admin/organizations/${orgID}/custom-domain`, { until });

/** domainBrandDTO — GET/PUT /api/admin/orgs/me/domain/brand. */
export interface DomainBrand {
  /** "" goes by the organization's name. */
  display_name: string;
  /** A look font key, "" for the site's. */
  font: string;
  terms_url: string;
  privacy_url: string;
}
export const getDomainBrand = (signal?: AbortSignal) => get<DomainBrand>(`${BASE}/brand`, signal ? { signal } : {});
export const putDomainBrand = (b: DomainBrand) => put<DomainBrand>(`${BASE}/brand`, b);
