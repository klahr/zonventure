import { get, patch, put, request } from "@zonventure/api";

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

export type QuoteStatus = "new" | "answered" | "won" | "lost";

/** internal/admin/quote_requests.go quoteRequestDTO */
export interface QuoteRequest {
  id: number;
  form: string;
  answers: QuoteAnswers;
  name: string;
  company: string;
  email: string;
  phone: string;
  lang: string;
  status: QuoteStatus;
  notes: string;
  created_at: string;
  updated_at: string;
}

/** GET /api/admin/requests: newest first, with how many each status has. */
export const listRequests = (status: string, signal?: AbortSignal) =>
  get<{ requests: QuoteRequest[]; counts: Partial<Record<QuoteStatus, number>> }>(
    `/api/admin/requests${status && status !== "all" ? `?status=${encodeURIComponent(status)}` : ""}`,
    signal ? { signal } : {},
  );

export const updateRequest = (id: number, body: { status?: QuoteStatus; notes?: string }) => patch<QuoteRequest>(`/api/admin/requests/${id}`, body);

export const deleteRequest = (id: number) => request<void>(`/api/admin/requests/${id}`, { method: "DELETE" });

/** GET/PUT /api/admin/orgs/me/requests: internal/admin/quote_requests.go quoteSettingsDTO. */
export interface QuoteSettings {
  open: boolean;
  intro: string;
  /** The form's address. */
  url: string;
  /** Where requests are emailed: the organisation's contact address. */
  contact: string;
  /** What the form leaves out: adventure kinds, "game_master", extras, languages. */
  off: string[];
}

export const getQuoteSettings = (signal?: AbortSignal) => get<QuoteSettings>("/api/admin/orgs/me/requests", signal ? { signal } : {});

export const putQuoteSettings = (body: { open: boolean; intro: string; off: string[] }) => put<QuoteSettings>("/api/admin/orgs/me/requests", body);
