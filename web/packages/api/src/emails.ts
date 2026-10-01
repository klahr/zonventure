// Emails in the organisation's look. Routes: internal/server/routes_admin.go;
// handlers: internal/admin/emails.go; themes and kinds: internal/emailtpl.

import { get, post, put } from "./http.ts";

export type Theme = "classic" | "adventure" | "bold" | "minimal";

export interface ThemeInfo {
  id: Theme;
  name: string;
  description: string;
}

/** An email there is a sample of, to preview. */
export interface Kind {
  id: string;
  name: string;
  audience: "players" | "designers";
}

export interface Preview {
  subject: string;
  html: string;
  text: string;
  theme: Theme;
}

export const themes = () => get<{ themes: ThemeInfo[]; kinds: Kind[] }>("/api/admin/email-themes");

/** A sample of one kind of email, as the caller's organisation — or an event — sends it; theme overrides. */
export const preview = (kind: string, opts: { theme?: Theme; eventId?: number; lang?: string; signal?: AbortSignal } = {}) => {
  const q = new URLSearchParams({ kind });
  if (opts.theme) q.set("theme", opts.theme);
  if (opts.lang) q.set("lang", opts.lang);
  if (opts.eventId) q.set("event_id", String(opts.eventId));
  return get<Preview>(`/api/admin/email-preview?${q}`, opts.signal ? { signal: opts.signal } : {});
};

/** What an arranger is writing to teams — or an email step sends — filled in as the first team gets it. */
export const previewDraft = (
  draft: { subject: string; body: string; theme?: Theme; event_id?: number; kind?: "team_message" | "game_email"; step?: string; lang?: string },
  signal?: AbortSignal,
) =>
  post<Preview>("/api/admin/email-preview", draft, signal ? { signal } : {});

/** The organisation's theme, and the language of what it sends outside any event. */
export const orgTheme = () => get<{ theme: Theme; language: string; languages: string[] }>("/api/admin/orgs/me/email-theme");
export const setOrgTheme = (theme: Theme) => put<void>("/api/admin/orgs/me/email-theme", { theme });
export const setOrgLanguage = (language: string) => put<void>("/api/admin/orgs/me/email-theme", { language });

/** The language the caller uses the console in: their notifications come in it. */
export const setMyLanguage = (language: string) => put<void>("/api/admin/me/language", { language });

/** An event's own theme; "" sends in the organisation's. */
export const setEventTheme = (eventId: number, theme: Theme | "") => put<void>(`/api/admin/events/${eventId}/email-theme`, { theme });

export interface SentEmail {
  id: number;
  subject: string;
  body: string;
  theme: Theme;
  sent_by?: string;
  sent_at: string;
  sent: number;
  skipped: number;
}

export interface EventEmails {
  emails: SentEmail[];
  /** The event's own theme, "" when it uses the organisation's. */
  theme: Theme | "";
  /** The theme its emails go out in. */
  theme_used: Theme;
  with_email: number;
  teams_total: number;
}

export const eventEmails = (eventId: number, signal?: AbortSignal) => get<EventEmails>(`/api/admin/events/${eventId}/emails`, signal ? { signal } : {});

/** Write to an event's teams — all, or those chosen. {{team}} and {{event}} are filled in. */
export const emailTeams = (eventId: number, msg: { subject: string; body: string; team_ids?: number[] }) =>
  post<{ id: number; sent: number; skipped: number }>(`/api/admin/events/${eventId}/emails`, msg);

export type NotificationKind = "mention" | "step_assigned" | "suggestion" | "review_requested";

export const prefs = () => get<{ prefs: Record<NotificationKind, boolean> }>("/api/admin/me/email-prefs");
export const setPrefs = (change: Partial<Record<NotificationKind, boolean>>) => put<{ prefs: Record<NotificationKind, boolean> }>("/api/admin/me/email-prefs", change);

/**
 * Send the caller, at their own address, what a preview shows — a sample
 * of a kind of email, or a draft — marked [Test] in its subject.
 */
export const sendMeTest = (
  req:
    | { sample: string; theme?: Theme; event_id?: number; lang?: string }
    | { subject: string; body: string; theme?: Theme; event_id?: number; kind?: "team_message" | "game_email"; step?: string; lang?: string },
) => post<{ to: string }>("/api/admin/email-test", req);
