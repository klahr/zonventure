import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Info } from "lucide-react";
import { get, readsAsAdmin } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Card, Dialog, ErrorBox, Facts, Field, ICON, Loading, Pill, Row, Table, errorText, relTime, toDate, useLoad } from "../../ui/index.ts";
import { EVENT, REASON, failed } from "./account-activity.tsx";

/** auditEventDTO, internal/admin/auditlog.go. */
interface AuthEvent {
  id: number;
  ts: string;
  event: string;
  ok: boolean;
  user_id?: number;
  username?: string;
  ip?: string;
  user_agent?: string;
  reason?: string;
}

/** adminActionDTO, internal/admin/admin_actions.go. */
interface AdminAction {
  id: number;
  ts: string;
  username?: string;
  action: string;
  target: string;
  target_name?: string;
}

// What each admin route does, by "METHOD pattern" as LogAdminActions
// (internal/admin/admin_actions.go) records it. Unlisted routes show raw.
const ACTION: Record<string, string> = {
  "POST /api/admin/users/{userID}/password": "Set a user's password",
  "POST /api/admin/users/{userID}/totp/disable": "Reset a user's two-factor sign-in",
  "POST /api/admin/users/{userID}/verify-email": "Verified an email address",
  "POST /api/admin/organizations": "Created an organization",
  "PATCH /api/admin/organizations/{orgID}": "Changed an organization",
  "DELETE /api/admin/organizations/{orgID}": "Deleted an organization",
  "POST /api/admin/organizations/{orgID}/members": "Added a member",
  "DELETE /api/admin/organizations/{orgID}/members/{userID}": "Removed a member",
  "DELETE /api/admin/organizations/{orgID}/invitations/{userID}": "Withdrew an invitation",
  "POST /api/admin/organizations/{orgID}/transfer-ownership": "Transferred ownership",
  "PUT /api/admin/organizations/{orgID}/require-totp": "2FA required for members",
  "POST /api/admin/organizations/{orgID}/stripe-connect/start": "Started Stripe onboarding",
  "POST /api/admin/organizations/{orgID}/stripe-connect/refresh": "Refreshed the Stripe status",
  "POST /api/admin/organizations/{orgID}/stripe-connect/dashboard": "Opened the Stripe dashboard",
  "POST /api/admin/organizations/{orgID}/trusted": "Invoice billing",
  "POST /api/admin/organizations/{orgID}/party-adventures": "Party adventures",
  "POST /api/admin/organizations/{orgID}/verified-creator": "Verified creator",
  "PUT /api/admin/organizations/{orgID}/publisher": "Catalog publisher",
  "POST /api/admin/organizations/{orgID}/logo": "Uploaded a logo",
  "DELETE /api/admin/organizations/{orgID}/logo": "Removed a logo",
  "POST /api/admin/organizations/{orgID}/assignments": "Added an event pass",
  "PATCH /api/admin/organizations/{orgID}/assignments/{assignmentID}": "Changed an event pass",
  "DELETE /api/admin/organizations/{orgID}/assignments/{assignmentID}": "Deleted an event pass",
  "POST /api/admin/organizations/{orgID}/teams/{teamID}/invitations/send": "Sent a team invitation",
  "POST /api/admin/organizations/{orgID}/events/{eventID}/cancel": "Cancelled an event",
  "POST /api/admin/parties/{eventID}/resend-host-link": "Resent a host link",
  "POST /api/admin/events/{eventID}/grant-slots": "Granted player slots",
  "POST /api/admin/purchases/{purchaseID}/confirm": "Confirmed a purchase",
  "POST /api/admin/purchases/{purchaseID}/refund": "Refunded a purchase",
  "POST /api/admin/purchases/{purchaseID}/billed": "Marked a purchase billed",
  "POST /api/admin/purchases/{purchaseID}/void": "Voided a purchase",
  "POST /api/admin/purchases/{purchaseID}/unvoid": "Undid a void",
  "POST /api/admin/ticket-purchases/{purchaseID}/refund": "Refunded a ticket sale",
  "PATCH /api/admin/ticket-purchases/{purchaseID}": "Changed a ticket order",
  "POST /api/admin/listing-reviews/{kind}/{id}/sign": "Signed a listing decision",
  "POST /api/admin/listing-reviews/{kind}/{id}/approve": "Approved a listing",
  "POST /api/admin/listing-reviews/{kind}/{id}/reject": "Rejected a listing",
  "POST /api/admin/listing-reviewers": "Listing reviewer",
  "PUT /api/admin/catalog/{slug}/published": "Listed in the catalog",
  "POST /api/admin/catalog/{slug}/shares": "Shared an adventure",
  "DELETE /api/admin/catalog/{slug}/shares/{orgID}": "Stopped sharing an adventure",
  "POST /api/admin/admins": "Created an admin",
  "DELETE /api/admin/admins/{userID}": "Deleted an admin",
  "PUT /api/admin/admins/{userID}/role": "Changed an admin's role",
  "POST /api/admin/admins/{userID}/reset-link": "Sent a password reset link",
  "PUT /api/admin/settings/signup": "Public sign-up",
  "PUT /api/admin/settings/admin-totp": "2FA required for admins",
  "PUT /api/admin/pricing": "Changed the pricing",
  "PUT /api/admin/legal": "Changed the legal texts",
  "POST /api/admin/legal/preview": "Previewed the legal texts",
  "POST /api/admin/migrate/import": "Imported an organization",
  "POST /api/admin/migrate/redirect-mode": "Redirect mode",
  "POST /api/admin/migrate/maintenance": "Maintenance mode",
  "POST /api/admin/system-update/check": "Checked for updates",
  "POST /api/admin/send-test-email": "Sent a test email",
  "PUT /api/admin/logs/level": "Log level",
  "POST /api/admin/oauth/approve": "Approved an AI assistant",
};
// The notes handlers add: which way a switch went, or a role.
const NOTE: Record<string, string> = { on: "on", off: "off", unbilled: "unbilled", admin: "Admin", support: "Support" };

/** "POST /api/admin/x/{id} (on)" → method + pattern, and the note. */
function parseAction(action: string): { route: string; note: string } {
  const m = /^(\S+ \S+)(?: \((.*)\))?$/.exec(action);
  return m ? { route: m[1]!, note: m[2] ?? "" } : { route: action, note: "" };
}

const PAGE = 100;
const MONO = { fontFamily: "var(--dz-font-mono)", fontSize: "var(--dz-text-sm)" };
const STAMP = { whiteSpace: "nowrap", fontFamily: "var(--dz-font-mono)", color: "var(--dz-muted)", fontSize: "var(--dz-text-xs)" };
// The kinds internal/auth/audit.go records.
const KINDS = Object.keys(EVENT);

interface Filters {
  event: string;
  ok: string;
  username: string;
  ip: string;
}
const NO_FILTERS: Filters = { event: "", ok: "", username: "", ip: "" };

/** Forensic timestamps: local, 24-hour, with seconds and the zone. */
function fmtTS(ts: string, locale: string): string {
  const d = toDate(ts);
  return d
    ? d.toLocaleString(locale, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false, timeZoneName: "short" })
    : ts;
}

/**
 * Login, set-password, magic-link and logout events, newest first, 100
 * at a time. The server scopes the rows: an org admin sees their org's
 * users, the platform admin everything.
 */
export function AuditTab() {
  const t = useT();
  const kind = (k: string) => (EVENT[k] ? t.t(EVENT[k]!) : k);
  const why = (r?: string) => (!r || r === "success" ? "" : REASON[r] ? t.t(REASON[r]!) : r);
  const { state } = useSession();
  const isAdmin = state.status === "authenticated" && readsAsAdmin(state.user);
  const locale = t.current();
  const [draft, setDraft] = useState<Filters>(NO_FILTERS);
  const [applied, setApplied] = useState<Filters>(NO_FILTERS);
  const [rows, setRows] = useState<AuthEvent[]>([]);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<AuthEvent | null>(null);
  const gen = useRef(0);

  const fetchPage = useCallback(async (f: Filters, before: number) => {
    const my = before ? gen.current : ++gen.current;
    setBusy(true);
    setErr("");
    const qs = new URLSearchParams({ limit: String(PAGE) });
    if (before) qs.set("before_id", String(before));
    if (f.event) qs.set("event", f.event);
    if (f.ok) qs.set("ok", f.ok);
    if (f.username.trim()) qs.set("username", f.username.trim());
    if (f.ip.trim()) qs.set("ip", f.ip.trim());
    try {
      const r = await get<{ events: AuthEvent[] | null }>("/api/admin/auth-events?" + qs.toString());
      if (my !== gen.current) return;
      const evs = r.events ?? [];
      setRows((old) => (before ? [...old, ...evs] : evs));
      setDone(evs.length < PAGE);
    } catch (e) {
      if (my === gen.current) setErr(t.t("Something went wrong: {{err}}", { err: errorText(e) }));
    } finally {
      if (my === gen.current) setBusy(false);
    }
  }, [t]);

  useEffect(() => { void fetchPage(applied, 0); }, [applied, fetchPage]);

  const apply = () => setApplied({ ...draft });
  const clear = () => { setDraft(NO_FILTERS); setApplied({ ...NO_FILTERS }); };
  const onEnter = (e: KeyboardEvent) => { if (e.key === "Enter") { e.preventDefault(); apply(); } };
  const filtered = JSON.stringify(applied) !== JSON.stringify(NO_FILTERS) || JSON.stringify(draft) !== JSON.stringify(NO_FILTERS);
  const who = (e: AuthEvent) => e.username || (e.user_id ? "#" + e.user_id : "—");

  // The platform admin can jump to the server logs around an event's
  // clock time; the logs view reads the prefilter once. Both views show
  // the browser's local time, so the minute is taken in it too.
  const toLogs = (e: AuthEvent) => {
    const d = toDate(e.ts);
    if (!d) return;
    const hhmm = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
    try { sessionStorage.setItem("zv_logs_prefilter", hhmm); } catch { /* private mode */ }
    navigate({ name: "logs" });
  };

  return (
    <>
      <Card title={t.t("Audit")} subtitle={t.t("Sign-ins, sign-outs, passwords, sign-in links, email verification and 2FA events. Newest first.")} flush>
        <div style={{ padding: "0 var(--dz-space-4)" }}>
        <Row>
          <Field label={t.t("Event")}>
            <select value={draft.event} onChange={(e) => setDraft({ ...draft, event: e.target.value })}>
              <option value="">{t.t("All events")}</option>
              {KINDS.map((k) => <option key={k} value={k}>{kind(k)}</option>)}
            </select>
          </Field>
          <Field label={t.t("Result")}>
            <select value={draft.ok} onChange={(e) => setDraft({ ...draft, ok: e.target.value })}>
              <option value="">{t.t("OK or fail")}</option>
              <option value="1">{t.t("OK only")}</option>
              <option value="0">{t.t("Fail only")}</option>
            </select>
          </Field>
          <Field label={t.t("User")}>
            <input type="text" style={{ width: "10rem" }} placeholder={t.t("substring")} value={draft.username} onKeyDown={onEnter} onChange={(e) => setDraft({ ...draft, username: e.target.value })} />
          </Field>
          <Field label={t.t("IP")}>
            <input type="text" style={{ width: "10rem" }} placeholder={t.t("substring")} value={draft.ip} onKeyDown={onEnter} onChange={(e) => setDraft({ ...draft, ip: e.target.value })} />
          </Field>
          <Row gap="sm">
            <Button size="sm" onClick={apply}>{t.t("Apply")}</Button>
            {filtered && <Button size="sm" variant="ghost" onClick={clear}>{t.t("Clear")}</Button>}
          </Row>
        </Row>
        </div>
        <Table
          caption={t.t("Audit")}
          rows={rows}
          rowKey={(e) => e.id}
          columns={[
            {
              key: "ts",
              header: t.t("Time"),
              cell: (e) =>
                isAdmin ? (
                  <a href={href({ name: "logs" })} title={t.t("Open server logs around this time")} onClick={(ev) => { ev.preventDefault(); toLogs(e); }}>
                    <span title={fmtTS(e.ts, locale)}>{relTime(e.ts, locale)}</span>
                    <div className="cs-wide" style={STAMP}>{fmtTS(e.ts, locale)}</div>
                  </a>
                ) : (
                  <>
                    <span title={fmtTS(e.ts, locale)}>{relTime(e.ts, locale)}</span>
                    <div className="cs-wide" style={STAMP}>{fmtTS(e.ts, locale)}</div>
                  </>
                ),
            },
            { key: "event", header: t.t("Event"), cell: (e) => <>{kind(e.event)}{failed(e) && <> <Pill tone="bad">{t.t("fail")}</Pill></>}</> },
            { key: "user", header: t.t("User"), cell: who },
            { key: "ip", header: t.t("IP"), wide: true, cell: (e) => <span style={MONO}>{e.ip || "—"}</span> },
            { key: "reason", header: t.t("Reason"), wide: true, cell: (e) => <span style={{ color: "var(--dz-muted)" }}>{why(e.reason)}</span> },
            {
              key: "details",
              header: <span className="cs-sr">{t.t("Details")}</span>,
              align: "end",
              cell: (e) => <Button size="icon" variant="ghost" icon={<Info size={ICON.sm} aria-hidden />} aria-label={t.t("Show details")} title={t.t("Show details")} onClick={() => setOpen(e)} />,
            },
          ]}
        />
      </Card>
      <Row>
        {!done && <Button disabled={busy} onClick={() => void fetchPage(applied, rows[rows.length - 1]?.id ?? 0)}>{t.t("Load more")}</Button>}
        <span style={{ color: err ? "var(--dz-danger)" : "var(--dz-muted)" }} role="status">
          {err || (busy ? t.t("Loading…") : done ? (rows.length === 0 ? t.t("No events match.") : t.t("End of log.")) : "")}
        </span>
      </Row>
      {open && (
        <Dialog title={t.t("Audit event #{{id}}", { id: open.id })} onClose={() => setOpen(null)} footer={<Button onClick={() => setOpen(null)}>{t.t("Close")}</Button>}>
          <Facts
            items={[
              { label: t.t("Time"), value: fmtTS(open.ts, locale) },
              { label: t.t("Event"), value: kind(open.event) },
              { label: t.t("Result"), value: open.ok ? t.t("ok") : t.t("fail") },
              { label: t.t("User"), value: who(open) },
              { label: t.t("IP"), value: open.ip || "—" },
              { label: t.t("Reason"), value: why(open.reason) || "—" },
              { label: t.t("User-Agent"), value: <span style={{ wordBreak: "break-all" }}>{open.user_agent || "—"}</span> },
            ]}
          />
        </Dialog>
      )}
      {isAdmin && <AdminActions username={applied.username.trim()} />}
    </>
  );
}

/** Changes made through the platform admin's routes, newest 100 first; `username` is the User filter. */
function AdminActions({ username }: { username: string }) {
  const t = useT();
  const locale = t.current();
  const load = useLoad(
    (signal) => get<{ actions: AdminAction[] | null }>("/api/admin/admin-actions" + (username ? "?" + new URLSearchParams({ username }).toString() : ""), { signal }),
    [username],
  );
  const rows = load.data?.actions ?? [];
  return (
    <Card title={t.t("Admin actions")} subtitle={t.t("Changes made by platform admins: flags, refunds, voids, pricing, deletions and the rest. Newest first.")} flush>
      {load.error && !load.data ? (
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      ) : !load.data ? (
        <Loading label={t.t("Loading…")} />
      ) : rows.length === 0 ? (
        <p style={{ padding: "0 var(--dz-space-4)", color: "var(--dz-muted)" }}>{t.t("No admin actions yet.")}</p>
      ) : (
        <Table
          caption={t.t("Admin actions")}
          rows={rows}
          rowKey={(a) => a.id}
          columns={[
            {
              key: "ts",
              header: t.t("Time"),
              cell: (a) => (
                <>
                  <span title={fmtTS(a.ts, locale)}>{relTime(a.ts, locale)}</span>
                  <div className="cs-wide" style={STAMP}>{fmtTS(a.ts, locale)}</div>
                </>
              ),
            },
            { key: "user", header: t.t("User"), cell: (a) => a.username || "—" },
            {
              key: "action",
              header: t.t("Action"),
              cell: (a) => {
                const { route, note } = parseAction(a.action);
                const label = ACTION[route];
                if (!label) return <span style={MONO}>{a.action}</span>;
                return (
                  <span title={a.action}>
                    {t.t(label)}
                    {note && <> <Pill tone={note === "off" || note === "unbilled" ? "neutral" : "good"}>{NOTE[note] ? t.t(NOTE[note]!) : note}</Pill></>}
                  </span>
                );
              },
            },
            {
              key: "target",
              header: t.t("Target"),
              wide: true,
              cell: (a) => (a.target_name ? <span title={a.target}>{a.target_name}</span> : <span style={{ ...MONO, wordBreak: "break-all" }}>{a.target}</span>),
            },
          ]}
        />
      )}
    </Card>
  );
}
