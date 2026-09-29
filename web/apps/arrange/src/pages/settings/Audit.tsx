import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Info } from "lucide-react";
import { get } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Card, Dialog, Facts, Field, ICON, Pill, Row, Table, errorText, relTime, toDate } from "../../ui/index.ts";

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

const PAGE = 100;
const MONO = { fontFamily: "var(--dz-font-mono)", fontSize: "var(--dz-text-sm)" };
const STAMP = { whiteSpace: "nowrap", fontFamily: "var(--dz-font-mono)", color: "var(--dz-muted)", fontSize: "var(--dz-text-xs)" };
// The kinds internal/auth/audit.go records.
const KINDS = ["login", "logout", "set_password", "magic_link", "email_verify"];

interface Filters {
  event: string;
  ok: string;
  username: string;
  ip: string;
}
const NO_FILTERS: Filters = { event: "", ok: "", username: "", ip: "" };

/** Forensic timestamps: local, 24-hour, with seconds. */
function fmtTS(ts: string, locale: string): string {
  const d = toDate(ts);
  return d ? d.toLocaleString(locale, { dateStyle: "medium", timeStyle: "medium", hour12: false }) : ts;
}

/**
 * Login, set-password, magic-link and logout events, newest first, 100
 * at a time. The server scopes the rows: an org admin sees their org's
 * users, the platform admin everything.
 */
export function AuditTab() {
  const t = useT();
  const { state } = useSession();
  const isAdmin = state.status === "authenticated" && state.user.role === "admin";
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
  // clock time; the logs view reads the prefilter once.
  const toLogs = (e: AuthEvent) => {
    const m = /[T ](\d{2}:\d{2})/.exec(e.ts || "");
    if (!m) return;
    try { sessionStorage.setItem("zv_logs_prefilter", m[1]!); } catch { /* private mode */ }
    navigate({ name: "logs" });
  };

  return (
    <>
      <Card title={t.t("Audit")} subtitle={t.t("Login, set-password, magic-link and logout events. Newest first.")} flush>
        <div style={{ padding: "0 var(--dz-space-4)" }}>
        <Row>
          <Field label={t.t("Event")}>
            <select value={draft.event} onChange={(e) => setDraft({ ...draft, event: e.target.value })}>
              <option value="">{t.t("All events")}</option>
              {KINDS.map((k) => <option key={k} value={k}>{t.t(k)}</option>)}
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
            { key: "event", header: t.t("Event"), cell: (e) => <>{t.t(e.event)}{!e.ok && <> <Pill tone="bad">{t.t("fail")}</Pill></>}</> },
            { key: "user", header: t.t("User"), cell: who },
            { key: "ip", header: t.t("IP"), wide: true, cell: (e) => <span style={MONO}>{e.ip || "—"}</span> },
            { key: "reason", header: t.t("Reason"), wide: true, cell: (e) => <span style={{ color: "var(--dz-muted)" }}>{e.reason || ""}</span> },
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
              { label: t.t("Event"), value: t.t(open.event) },
              { label: t.t("Result"), value: open.ok ? t.t("ok") : t.t("fail") },
              { label: t.t("User"), value: who(open) },
              { label: t.t("IP"), value: open.ip || "—" },
              { label: t.t("Reason"), value: open.reason || "—" },
              { label: t.t("User-Agent"), value: <span style={{ wordBreak: "break-all" }}>{open.user_agent || "—"}</span> },
            ]}
          />
        </Dialog>
      )}
    </>
  );
}
