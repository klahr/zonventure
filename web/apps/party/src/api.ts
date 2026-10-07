import { request } from "@zonventure/api";

/** A party an organisation offers: internal/admin/party.go PublicPartyOfferDTO. */
export interface Offer {
  id: number;
  adventure_slug: string;
  title: string;
  blurb: string;
  base_cents: number;
  per_kid_cents: number;
  currency: string;
  max_kids: number;
  org_name: string;
  org_slug: string;
  payments_open: boolean;
}

const enc = encodeURIComponent;

export const getOffer = (slug: string, id: string) => request<Offer>(`/api/o/${enc(slug)}/parties/${enc(id)}`);

export const book = (slug: string, id: string, body: { email: string; kids: number; party_name: string; consent: boolean }) =>
  request<{ checkout_url: string }>(`/api/o/${enc(slug)}/parties/${enc(id)}/book`, { method: "POST", body });

export const resend = (email: string) => request<void>("/api/party/resend", { method: "POST", body: { email } });

/** The join page's view of a hosted event: internal/admin/hosted.go joinInfoDTO. */
export interface JoinInfo {
  name: string;
  adventure_title: string;
  org_name: string;
  status: "open" | "full" | "not_ready" | "ended";
  /** A party adventure's starter theme, for its cover picture. */
  theme?: string;
  /** The adventure's own cover picture, in place of the theme's. */
  cover_url?: string;
}

export const getJoin = (token: string) => request<JoinInfo>(`/api/join/${enc(token)}`);

export const join = (token: string, team: string, headcount: number) =>
  request<{ play_url: string }>(`/api/join/${enc(token)}`, { method: "POST", body: { team, headcount } });

/** A price in minor units, in the reader's locale. */
export function money(cents: number, currency: string, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency: currency.toUpperCase(), maximumFractionDigits: cents % 100 === 0 ? 0 : 2 }).format(cents / 100);
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency.toUpperCase()}`;
  }
}
