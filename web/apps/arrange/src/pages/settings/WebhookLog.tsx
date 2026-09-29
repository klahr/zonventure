import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { useTopBarTitle } from "../../topbar.tsx";
import { Button, Callout, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, Row, errorText, fmtDateTime, relTime, useLoad, type Tone } from "../../ui/index.ts";
import { listDeliveries, listWebhooks, type WebhookDelivery } from "./integrations-api.ts";
import "./integrations-webhooks.css";

/**
 * One endpoint's delivery history: every attempt, the status it got
 * back, and the exact JSON that was posted. Re-reads every 4 s while a
 * delivery is still waiting, keeping rows the user opened open.
 */
export function WebhookLogPage({ endpoint }: { endpoint: number }) {
  const t = useT();
  const eps = useLoad((signal) => listWebhooks(signal), [endpoint]);
  const ep = eps.data?.endpoints?.find((e) => e.id === endpoint) ?? null;
  const name = ep ? ep.name || t.t("(unnamed endpoint)") : "";

  useTopBarTitle(name || null);
  // Someone else's (or a deleted) endpoint: back to the list, as the
  // classic console does.
  useEffect(() => {
    if (eps.data && !ep) navigate({ name: "settings", tab: "integrations" });
  }, [eps.data, ep]);

  const back = { href: href({ name: "settings", tab: "integrations" }), label: t.t("Back to webhooks") };
  if (eps.error && !eps.data) return <Page><PageHeader title={t.t("Delivery log")} back={back} /><ErrorBox message={eps.error} retry={eps.reload} retryLabel={t.t("Retry")} /></Page>;
  if (!ep) return <Page><PageHeader title={t.t("Delivery log")} back={back} /><Loading label={t.t("Loading…")} /></Page>;

  return (
    <Page>
      <PageHeader
        back={back}
        title={t.t("Delivery log: {{name}}", { name })}
        subtitle={<span className="wh-url">{ep.url}</span>}
      />
      <Deliveries endpoint={endpoint} />
    </Page>
  );
}

function Deliveries({ endpoint }: { endpoint: number }) {
  const t = useT();
  const [status, setStatus] = useState("");
  const [rows, setRows] = useState<WebhookDelivery[]>([]);
  const [next, setNext] = useState(0);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<Set<number>>(() => new Set());
  const gen = useRef(0);

  // reset: a fresh first page (loud: clears and shows loading; quiet:
  // the poll, which swaps the list only when the answer is in).
  const fetchPage = useCallback(
    async (mode: "loud" | "quiet" | "more", before = 0) => {
      const my = mode === "more" ? gen.current : ++gen.current;
      if (mode === "loud") {
        setRows([]);
        setLoading(true);
        setErr("");
      }
      try {
        const r = await listDeliveries({ endpoint, status, before: mode === "more" ? before : 0 });
        if (my !== gen.current) return;
        const incoming = r.deliveries ?? [];
        setRows((old) => (mode === "more" ? [...old, ...incoming] : incoming));
        setNext(r.next_before ?? 0);
        setErr("");
      } catch (e) {
        // A dropped poll keeps what is on screen; the next tick retries.
        if (mode !== "quiet" && my === gen.current) setErr(t.t("Could not load deliveries ({{status}}).", { status: errorText(e) }));
      } finally {
        if (my === gen.current) setLoading(false);
      }
    },
    [endpoint, status, t],
  );

  useEffect(() => { void fetchPage("loud"); }, [fetchPage]);

  const pending = rows.some((d) => d.status === "pending");
  useEffect(() => {
    if (!pending) return;
    const timer = setTimeout(function tick() {
      if (document.hidden) return;
      void fetchPage("quiet");
    }, 4000);
    return () => clearTimeout(timer);
  }, [pending, rows, fetchPage]);

  // A hidden tab skips its tick; coming back resumes the poll.
  useEffect(() => {
    const onVis = () => { if (!document.hidden && pending) void fetchPage("quiet"); };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, [pending, fetchPage]);

  const toggle = (id: number) => setOpen((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });

  return (
    <>
      <Row>
        {(rows.length > 0 || status || loading) && (
        <Field label={t.t("Status")} inline>
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">{t.t("Any status")}</option>
            <option value="pending">{t.t("Waiting")}</option>
            <option value="delivered">{t.t("Delivered")}</option>
            <option value="dead">{t.t("Dead")}</option>
          </select>
        </Field>
        )}
        <Button size="sm" icon={<RefreshCw size={ICON.sm} aria-hidden />} onClick={() => void fetchPage("loud")}>{t.t("Refresh")}</Button>
      </Row>
      {err && <ErrorBox message={err} retry={() => void fetchPage("loud")} retryLabel={t.t("Retry")} />}
      {loading && rows.length === 0 ? (
        <Loading label={t.t("Loading…")} />
      ) : rows.length === 0 && !err ? (
        <Empty title={t.t("No deliveries yet. Send a test from the Webhooks page to see one land here.")} />
      ) : (
        <ul className="wh-log">
          {rows.map((d) => <DeliveryRow key={d.id} d={d} open={open.has(d.id)} onToggle={() => toggle(d.id)} />)}
        </ul>
      )}
      {rows.length > 0 && (
        <Row>
          {next > 0 && <Button onClick={() => void fetchPage("more", next)}>{t.t("Load more")}</Button>}
          <span className="wh-muted" role="status">{t.plural(rows.length, { one: t.t("Showing {{n}} delivery"), other: t.t("Showing {{n}} deliveries") })}</span>
        </Row>
      )}
    </>
  );
}

function DeliveryRow({ d, open, onToggle }: { d: WebhookDelivery; open: boolean; onToggle: () => void }) {
  const t = useT();
  const locale = t.current();
  const statusTone: Tone = d.status === "delivered" ? "good" : d.status === "dead" ? "bad" : "neutral";
  const statusText = d.status === "delivered" ? t.t("Delivered") : d.status === "pending" ? t.t("Waiting") : d.status === "dead" ? t.t("Dead") : d.status;
  const detailID = `wh-d-${d.id}`;
  let pretty = d.payload;
  if (open) {
    try { pretty = JSON.stringify(JSON.parse(d.payload), null, 2); } catch { /* show it raw */ }
  }
  return (
    <li className="wh-row">
      <button type="button" className="wh-row__head" aria-expanded={open} aria-controls={detailID} onClick={onToggle}>
        {open ? <ChevronDown size={ICON.sm} aria-hidden /> : <ChevronRight size={ICON.sm} aria-hidden />}
        <Pill tone={statusTone}>{statusText}</Pill>
        {d.last_status != null ? (
          <Pill tone={d.last_status >= 200 && d.last_status < 300 ? "good" : "bad"}>{String(d.last_status)}</Pill>
        ) : d.attempts > 0 ? (
          <Pill tone="bad">{t.t("No response")}</Pill>
        ) : null}
        <code className="wh-kind">{d.event_kind}</code>
        <span className="wh-muted wh-small" title={fmtDateTime(d.created_at, locale)}>{relTime(d.created_at, locale)}</span>
      </button>
      {open && (
        <div id={detailID} className="wh-row__detail">
          <p className="wh-muted wh-small"><strong>X-Zv-Delivery-Id: </strong><code>{d.delivery_uuid}</code></p>
          <p className="wh-muted wh-small">
            <strong>{t.t("Attempts: ")}</strong>{d.attempts}
            {d.delivered_at && <> · <strong>{t.t("Delivered at: ")}</strong>{fmtDateTime(d.delivered_at, locale)}</>}
            {d.status === "pending" && d.next_attempt_at && <> · <strong>{t.t("Next attempt: ")}</strong>{fmtDateTime(d.next_attempt_at, locale)}</>}
          </p>
          {d.last_error && <Callout variant="warning"><span className="wh-pre">{d.last_error}</span></Callout>}
          <pre className="wh-payload">{pretty}</pre>
        </div>
      )}
    </li>
  );
}
