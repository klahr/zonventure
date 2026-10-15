import { request, type play } from "@zonventure/api";

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
  /** How many teams a game of the booked party takes. */
  teams: number;
  org_name: string;
  org_slug: string;
  payments_open: boolean;
  /** The adventure's look, starter theme and cover picture. */
  look?: play.Brand;
  theme?: string;
  cover_url?: string;
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
  /** The adventure's look, so the page matches the game. */
  look?: play.Brand;
}

export const getJoin = (token: string) => request<JoinInfo>(`/api/join/${enc(token)}`);

export const join = (token: string, team: string) =>
  request<{ play_url: string }>(`/api/join/${enc(token)}`, { method: "POST", body: { team } });

/** A price in minor units, in the reader's locale. */
export function money(cents: number, currency: string, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency: currency.toUpperCase(), maximumFractionDigits: cents % 100 === 0 ? 0 : 2 }).format(cents / 100);
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency.toUpperCase()}`;
  }
}

/** An organisation's request form: internal/admin/quote_requests.go GetQuoteForm. */
export interface QuoteForm {
  org_name: string;
  org_slug: string;
  intro: string;
  currency: string;
  /** When the form loaded, signed: sent back with the request. */
  started: string;
  /** The organisation's brand: its colours and title typeface. */
  look?: play.Brand;
  logo_url?: string;
  cover_url?: string;
  /** What the organisation doesn't offer: adventure kinds, "game_master", extras, languages. */
  off?: string[];
}

/** The company form's answers: internal/admin/quote_requests.go quoteAnswers. */
export interface QuoteAnswers {
  occasion: string;
  date?: string;
  month?: string;
  time_of_day?: string;
  duration?: string;
  people: number;
  city: string;
  venue?: string;
  help_choose?: boolean;
  scope: string;
  game_master?: boolean;
  extras?: string[];
  languages?: string[];
  budget?: string;
  message?: string;
}

export const getQuoteForm = (slug: string) => request<QuoteForm>(`/api/o/${enc(slug)}/request`);

export const sendQuoteRequest = (
  slug: string,
  body: { answers: QuoteAnswers; name: string; company: string; email: string; phone: string; website: string; started: string },
) => request<{ ok: boolean }>(`/api/o/${enc(slug)}/request`, { method: "POST", body });
