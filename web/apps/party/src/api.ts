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

/** A station card: a zone of the adventure, as a card the host hides. */
export interface Station {
  zone: string;
  name: string;
  code: string;
  url: string;
}

export interface Team {
  id: number;
  name: string;
  size: number;
  joined: number;
  token: string;
  join_url: string;
  voucher: string;
  finished: boolean;
  score: number;
}

/** The host page's party: internal/admin/party.go hostPartyDTO. */
export interface Party {
  name: string;
  adventure_title: string;
  org_name: string;
  state: "scheduled" | "running" | "paused" | "completed" | "cancelled";
  kids: number;
  valid_until: string;
  expired: boolean;
  stations: Station[];
  teams: Team[];
  live_url: string;
  play_url: string;
  mode: "qr" | "park";
  cards_ok: boolean;
  park_ok: boolean;
  values: HostValue[];
  editables: Editable[];
  places: Place[];
}

/** A word the host fills in: internal/admin/party_adapt.go hostValueDTO. */
export interface HostValue {
  slug: string;
  label: string;
  default: string;
  value: string;
}

/** A host's version of a text or quiz question. */
export interface BlockOverride {
  text?: string;
  options?: string[];
  answer?: string;
}

/** Something the host may rewrite: a text, a quiz question, or a typed answer (no kind). */
export interface Editable {
  key: string;
  node: string;
  step: string;
  kind?: "text" | "quiz";
  text?: string;
  options?: string[];
  answer?: string;
  accepts?: string[];
  mine?: BlockOverride;
  my_accepts?: string[];
}

/** One of the adventure's places, for a party in a park. */
export interface Place {
  slug: string;
  name: string;
  movable: boolean;
  lat: number;
  lng: number;
  radius_m: number;
  placed: boolean;
}

const enc = encodeURIComponent;

export const getOffer = (slug: string, id: string) => request<Offer>(`/api/o/${enc(slug)}/parties/${enc(id)}`);

export const book = (slug: string, id: string, body: { email: string; kids: number; party_name: string; consent: boolean }) =>
  request<{ checkout_url: string }>(`/api/o/${enc(slug)}/parties/${enc(id)}/book`, { method: "POST", body });

export const resend = (email: string) => request<void>("/api/party/resend", { method: "POST", body: { email } });

export const getParty = (token: string) => request<Party>(`/api/party/${enc(token)}`);

export const addTeam = (token: string, name: string, size: number) =>
  request<{ id: number }>(`/api/party/${enc(token)}/teams`, { method: "POST", body: { name, size } });

export const removeTeam = (token: string, id: number) => request<void>(`/api/party/${enc(token)}/teams/${id}`, { method: "DELETE" });

export const partyAction = (token: string, action: "start" | "pause" | "resume" | "end") =>
  request<void>(`/api/party/${enc(token)}/${action}`, { method: "POST" });

export const personalise = (token: string, body: { values: Record<string, string>; blocks: Record<string, BlockOverride>; answers: Record<string, string[]> }) =>
  request<void>(`/api/party/${enc(token)}/personalise`, { method: "PUT", body });

export const setMode = (token: string, mode: "qr" | "park") => request<void>(`/api/party/${enc(token)}/mode`, { method: "PUT", body: { mode } });

export const placeAt = (token: string, slug: string, lat: number, lng: number) =>
  request<void>(`/api/party/${enc(token)}/places/${enc(slug)}`, { method: "PUT", body: { lat, lng } });

export const shiftTo = (token: string, lat: number, lng: number) => request<void>(`/api/party/${enc(token)}/places/shift`, { method: "POST", body: { lat, lng } });

export const stationQR = (token: string, zone: string) => `/api/party/${enc(token)}/qr?station=${enc(zone)}`;
export const teamQR = (token: string, id: number) => `/api/party/${enc(token)}/qr?team=${id}`;

/** A price in minor units, in the reader's locale. */
export function money(cents: number, currency: string, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency: currency.toUpperCase(), maximumFractionDigits: cents % 100 === 0 ? 0 : 2 }).format(cents / 100);
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency.toUpperCase()}`;
  }
}
