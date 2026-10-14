import { siteCurrency } from "@zonventure/api";

/**
 * Times, durations and money as the console shows them. The server
 * writes timestamps two ways — RFC 3339, and "YYYY-MM-DD HH:MM:SS" in
 * UTC from now_text() — and toDate reads both.
 */
export function toDate(s: string | null | undefined): Date | null {
  if (!s) return null;
  const iso = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}(:\d{2})?$/.test(s) ? s.replace(" ", "T") + "Z" : s;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * The viewer's own regional variant of the app language: "en" in an
 * en-GB browser formats as "16 Oct 2026", not the US "Oct 16, 2026".
 */
export function regional(locale?: string): string | undefined {
  if (!locale || typeof navigator === "undefined") return locale;
  const lang = locale.toLowerCase().split("-")[0];
  return (navigator.languages ?? [navigator.language]).find((l) => l.toLowerCase().split("-")[0] === lang) ?? locale;
}

/** " (Europe/Stockholm)": the viewer's time zone, for a time that people elsewhere also read. */
export function zoneSuffix(): string {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return tz ? ` (${tz})` : "";
}

/** "29 Sep 2026, 09:06" — 24-hour, in the viewer's own zone. */
export function fmtDateTime(s: string | null | undefined, locale?: string): string {
  const d = toDate(s);
  return d ? d.toLocaleString(regional(locale), { dateStyle: "medium", timeStyle: "short", hour12: false }) : "—";
}

export function fmtDate(s: string | null | undefined, locale?: string): string {
  const d = toDate(s);
  return d ? d.toLocaleDateString(regional(locale), { dateStyle: "medium" }) : "—";
}

/** Only the time today; the date too otherwise. */
export function fmtWhen(s: string | null | undefined, locale?: string): string {
  const d = toDate(s);
  if (!d) return "—";
  return d.toDateString() === new Date().toDateString()
    ? d.toLocaleTimeString(regional(locale), { hour: "2-digit", minute: "2-digit", hour12: false })
    : fmtDateTime(s, locale);
}

/** "just now", "5m ago", "3h ago", else the date. */
export function fmtAgo(s: string | null | undefined, t: (k: string, v?: Record<string, string | number>) => string, locale?: string): string {
  const d = toDate(s);
  if (!d) return "—";
  const sec = (Date.now() - d.getTime()) / 1000;
  if (sec < 60) return t("just now");
  if (sec < 3600) return t("{{n}}m ago", { n: Math.round(sec / 60) });
  if (sec < 86400) return t("{{n}}h ago", { n: Math.round(sec / 3600) });
  return fmtDateTime(s, locale);
}

/** "in 3 days", "2 hours ago": the nearest unit, in the viewer's language. */
export function relTime(s: string | null | undefined, locale?: string): string {
  const d = toDate(s);
  if (!d) return "";
  const sec = (d.getTime() - Date.now()) / 1000;
  const abs = Math.abs(sec);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [["year", 31536000], ["month", 2592000], ["week", 604800], ["day", 86400], ["hour", 3600], ["minute", 60]];
  for (const [unit, size] of units) if (abs >= size) return rtf.format(Math.round(sec / size), unit);
  return rtf.format(0, "minute");
}

/** 75 → "1:15", 3700 → "1:01:40", past a day "2d 03:04:05". */
export function fmtDuration(totalSeconds: number): string {
  const sec = Math.max(0, Math.round(totalSeconds));
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  const p = (n: number) => String(n).padStart(2, "0");
  if (d > 0) return `${d}d ${p(h)}:${p(m)}:${p(s)}`;
  return h > 0 ? `${h}:${p(m)}:${p(s)}` : `${m}:${p(s)}`;
}

/** Minor units to "49,00 kr" in the viewer's locale; no currency is the site's. */
export function fmtMoney(cents: number, currency?: string, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: "currency", currency: (currency || siteCurrency()).toUpperCase() }).format(cents / 100);
}

/** The site currency's symbol as written beside a price input: "kr", "€". */
export function currencySymbol(locale?: string): string {
  return new Intl.NumberFormat(locale, { style: "currency", currency: siteCurrency(), currencyDisplay: "narrowSymbol" })
    .formatToParts(0).find((p) => p.type === "currency")?.value ?? siteCurrency();
}

/** payments.PlatformFee.Compute: the platform's cut of a sale of price, kept below the price. */
export function platformFeeOn(p: { fee_percent_bps?: number; fee_fixed_cents?: number; fee_min_cents?: number }, price: number): number {
  const bps = p.fee_percent_bps ?? 0, fixed = p.fee_fixed_cents ?? 0, min = p.fee_min_cents ?? 0;
  if (!(bps || fixed || min) || price <= 0) return 0;
  const fee = Math.max(Math.floor((price * bps) / 10000) + fixed, min);
  return Math.max(0, Math.min(fee, price - 1));
}

/** An <input type="datetime-local"> value for a timestamp, in local time. */
export function toLocalInput(s: string | null | undefined): string {
  const d = toDate(s);
  if (!d) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
