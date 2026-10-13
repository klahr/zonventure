import { useEffect, useRef, useState } from "react";
import { ApiError, get, post, readsAsAdmin } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, Empty, ErrorBox, Loading, Pill, Table, fmtDate, fmtDateTime, fmtMoney, useDialogs, useLoad, useToast, type Tone } from "../../ui/index.ts";

/** ticketSaleDTO, internal/admin/ticket_sales.go. */
interface Sale {
  id: number;
  kind: "ticket" | "party" | "hosted" | "hosted_blocks" | string;
  state: "pending" | "paid" | "failed" | "refunded" | string;
  event_id?: number;
  event_name?: string;
  adventure_title?: string;
  org_name?: string;
  buyer_email: string;
  team_size: number;
  blocks: number;
  teams?: number;
  amount_cents: number;
  currency: string;
  application_fee_cents: number;
  refundable: boolean;
  invoice_url?: string;
  created_at: string;
  paid_at?: string;
  refunded_at?: string;
  /** A refund asked of Stripe that its webhook hasn't confirmed yet. */
  refund_pending?: boolean;
  /** A checkout still pending a day after it began. */
  abandoned?: boolean;
}

type Filter = "all" | "paid" | "refunded" | "abandoned";

/**
 * What buyers paid for: self-guided tickets, party bookings, hosted
 * passes and more teams for one. An org sees its own; the platform
 * admin sees every org's, or orgId's on its admin page, and refunds them.
 */
export function TicketSalesCard({ orgId }: { orgId?: number | undefined } = {}) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state } = useSession();
  const isAdmin = state.status === "authenticated" && readsAsAdmin(state.user);
  const locale = t.current();
  const url = orgId ? `/api/admin/ticket-purchases?org_id=${orgId}` : "/api/admin/ticket-purchases";
  const load = useLoad((signal) => get<{ sales: Sale[] | null }>(url, { signal }), [url]);
  const [filter, setFilter] = useState<Filter>("all");
  const [busy, setBusy] = useState<number | null>(null);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const sales = load.data.sales ?? [];
  const shown = sales.filter((s) => (filter === "abandoned" ? !!s.abandoned : !s.abandoned && (filter === "all" || s.state === filter)));
  const money = (s: Sale) => fmtMoney(s.amount_cents, s.currency || "SEK", locale);

  const kinds: Record<string, string> = {
    ticket: t.t("Self-guided ticket"),
    party: t.t("Party adventure booking"),
    hosted: t.t("Hosted pass"),
    hosted_blocks: t.t("More teams"),
  };
  const what = (s: Sale) =>
    s.kind === "hosted" || s.kind === "hosted_blocks"
      ? t.plural(s.teams ?? 0, { one: t.t("{{n}} team per game"), other: t.t("{{n}} teams per game") })
      : s.kind === "party"
        ? t.plural(s.team_size, { one: t.t("{{n}} child"), other: t.t("{{n}} children") })
        : t.plural(s.team_size, { one: t.t("{{n}} player"), other: t.t("{{n}} players") });

  const refund = async (s: Sale) => {
    // A wrong password asks again rather than dropping the refund.
    let error: string | undefined;
    for (;;) {
      const pw = await dialogs.password({
        title: t.t("Refund"),
        message:
          s.kind === "ticket"
            ? t.t("Refund {{amount}} to {{email}}? Their team is taken out of the game once the refund is confirmed.", { amount: money(s), email: s.buyer_email })
            : s.kind === "party" || s.kind === "hosted"
              ? t.t("Refund {{amount}} to {{email}}? The event is called off once the refund is confirmed.", { amount: money(s), email: s.buyer_email })
              : t.t("Refund {{amount}} to {{email}}? The event holds fewer teams once the refund is confirmed.", { amount: money(s), email: s.buyer_email }),
        confirmLabel: t.t("Refund"),
        danger: true,
        ...(error ? { error } : {}),
      });
      if (!pw) return;
      setBusy(s.id);
      try {
        await post(`/api/admin/ticket-purchases/${s.id}/refund`, undefined, { headers: { "X-Confirm-Password": pw } });
        break;
      } catch (e) {
        setBusy(null);
        if (e instanceof ApiError && e.status === 401) {
          error = t.t("Wrong password. Try again.");
          continue;
        }
        toast.err(e);
        return;
      }
    }
    toast.ok(t.t("Refund initiated. Confirming with Stripe…"));
    load.reload();
    // The row flips with Stripe's charge.refunded webhook; look for up
    // to 30 s so the operator gets a clear answer.
    const deadline = Date.now() + 30_000;
    const poll = async () => {
      if (!alive.current) return;
      if (Date.now() > deadline) {
        toast.err(t.t("Refund still processing — check Stripe and refresh in a minute."));
        setBusy(null);
        load.reload();
        return;
      }
      try {
        const r = await get<{ sales: Sale[] | null }>(url);
        if (r.sales?.find((x) => x.id === s.id)?.state === "refunded") {
          toast.ok(t.t("Refund confirmed."));
          setBusy(null);
          load.reload();
          return;
        }
      } catch {
        /* try again next tick */
      }
      setTimeout(poll, 2000);
    };
    setTimeout(poll, 2000);
  };

  // A paid sale the org can't refund here: it wasn't paid into its own Stripe account.
  const operatorRefunds = (s: Sale) => !isAdmin && s.state === "paid" && !s.refundable && !s.refund_pending;

  const statePill = (s: Sale) => {
    if (s.refund_pending) return <Pill tone="warn">{t.t("Refund pending")}</Pill>;
    const tone: Tone = s.state === "paid" ? "good" : s.state === "pending" ? "warn" : s.state === "refunded" || s.state === "failed" ? "bad" : "neutral";
    return <Pill tone={tone}>{t.t(s.state)}</Pill>;
  };

  const filters = ([
    { key: "all", label: t.t("All") },
    { key: "paid", label: t.t("Paid") },
    { key: "refunded", label: t.t("Refunded") },
    ...(sales.some((s) => s.abandoned) ? [{ key: "abandoned", label: t.t("Abandoned checkouts") }] : []),
  ] as { key: Filter; label: string }[]);

  return (
    <Card
      title={t.t("Ticket sales")}
      subtitle={
        isAdmin && orgId
          ? t.t("Self-guided tickets, party adventure bookings and hosted passes. Refunds go back to the buyer's card.")
          : isAdmin
          ? t.t("Self-guided tickets, party adventure bookings and hosted passes, across all orgs. Refunds go back to the buyer's card.")
          : t.t("Self-guided tickets and party adventure bookings sold for your events, and hosted passes you bought. Sales paid into your own Stripe account can be refunded here, back to the buyer's card; for any other, ask the platform operator, whose contact address is in the terms.")
      }
      flush
    >
      {sales.length > 0 && (
        <div style={{ padding: "0 var(--dz-space-4)" }}>
          <div className="cs-row cs-row--sm" role="group" aria-label={t.t("Filter")}>
            {filters.map((f) => (
              <Button key={f.key} size="sm" variant={filter === f.key ? "primary" : "ghost"} aria-pressed={filter === f.key} onClick={() => setFilter(f.key)}>{f.label}</Button>
            ))}
          </div>
        </div>
      )}
      {sales.length === 0 ? (
        <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><Empty title={t.t("No ticket sales yet.")} /></div>
      ) : shown.length === 0 ? (
        <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><Empty title={t.t("Nothing matches this filter.")} /></div>
      ) : (
        <Table
          caption={t.t("Ticket sales")}
          rows={shown}
          rowKey={(s) => s.id}
          columns={[
            {
              key: "what",
              header: t.t("Sale"),
              cell: (s) => (
                <div className="acct-item__text">
                  <strong>{s.event_name || s.adventure_title || kinds[s.kind] || s.kind}</strong>
                  <span className="acct-item__meta" title={fmtDateTime(s.created_at, locale)}>
                    {kinds[s.kind] ?? s.kind} · {what(s)} · {fmtDate(s.created_at, locale)}
                  </span>
                </div>
              ),
            },
            ...(isAdmin && !orgId ? [{ key: "org", header: t.t("Org"), wide: true, cell: (s: Sale) => s.org_name || "—" }] : []),
            { key: "buyer", header: t.t("Buyer"), wide: true, cell: (s) => s.buyer_email },
            { key: "total", header: t.t("Total"), align: "end" as const, cell: (s) => money(s) },
            { key: "state", header: t.t("State"), cell: statePill },
            ...(shown.some((s) => s.refundable || operatorRefunds(s))
              ? [{
                  key: "actions",
                  header: <span className="cs-sr">{t.t("Actions")}</span>,
                  align: "end" as const,
                  cell: (s: Sale) =>
                    s.refundable ? (
                      <Button size="sm" loading={busy === s.id} onClick={() => void refund(s)}>{t.t("Refund")}</Button>
                    ) : operatorRefunds(s) ? (
                      <a className="acct-item__meta" href="/terms" target="_blank" rel="noopener" title={t.t("The platform operator's contact address is in the terms.")}>
                        {t.t("Refunds: ask the platform operator")}
                      </a>
                    ) : null,
                }]
              : []),
          ]}
        />
      )}
    </Card>
  );
}
