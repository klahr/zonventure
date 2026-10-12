import { useEffect, useRef, useState } from "react";
import { Coins } from "lucide-react";
import { get, post } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, href } from "../../router.ts";
import { Button, Card, Empty, ErrorBox, ICON, Loading, Pill, Row, Table, fmtDate, fmtDateTime, fmtMoney, useDialogs, useLoad, useToast, type Tone } from "../../ui/index.ts";
import { SPOTS } from "@zonventure/ui";

/** purchaseDTO, internal/admin/purchases.go. */
interface Purchase {
  id: number;
  org_id: number;
  adventure_slug: string;
  adventure_title?: string;
  max_players: number;
  amount_cents: number;
  currency: string;
  provider: "stripe" | "invoice" | "free" | string;
  state: "pending" | "paid" | "refunded" | string;
  invoice_url?: string;
  created_at: string;
  billed_at?: string;
  voided_at?: string;
  pre_migration?: boolean;
}

type Filter = "all" | "tobill" | "invoice" | "stripe" | "free";

function matches(p: Purchase, f: Filter): boolean {
  switch (f) {
    case "tobill":
      return p.provider === "invoice" && p.state === "paid" && !p.billed_at && !p.voided_at;
    case "invoice":
    case "stripe":
    case "free":
      return p.provider === f;
    default:
      return true;
  }
}

/**
 * Event-pass purchases. An org sees its own; the platform admin sees
 * every org's, filtered to what still needs invoicing, and can mark
 * invoices billed, void them, or refund Stripe charges.
 */
export function PurchasesTab() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state } = useSession();
  const isAdmin = state.status === "authenticated" && state.user.role === "admin";
  const locale = t.current();
  const load = useLoad((signal) => get<{ purchases: Purchase[] | null }>("/api/admin/purchases", { signal }), []);
  const [filter, setFilter] = useState<Filter>(isAdmin ? "tobill" : "all");
  const [busy, setBusy] = useState<number | "portal" | null>(null);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const purchases = load.data.purchases ?? [];
  const shown = purchases.filter((p) => matches(p, filter));
  const money = (p: Purchase) => fmtMoney(p.amount_cents, p.currency || "SEK", locale);
  const hasStripePaid = purchases.some((p) => p.state === "paid" && p.provider === "stripe");

  const portal = async () => {
    setBusy("portal");
    try {
      const r = await post<{ url?: string }>("/api/admin/me/portal-session");
      if (r?.url) {
        location.href = r.url;
        return;
      }
      toast.err(t.t("Could not open billing portal — try again."));
    } catch (e) {
      toast.err(e);
    }
    setBusy(null);
  };

  const act = async (p: Purchase, fn: () => Promise<unknown>, ok: string) => {
    setBusy(p.id);
    try {
      await fn();
      toast.ok(ok);
      load.reload();
    } catch (e) {
      toast.err(e);
    }
    setBusy(null);
  };

  const refund = async (p: Purchase) => {
    const pw = await dialogs.password({
      title: t.t("Refund"),
      message: t.t('Refund {{amount}} to the customer? The state flips to "refunded" once Stripe confirms.', { amount: money(p) }),
      confirmLabel: t.t("Refund"),
      danger: true,
    });
    if (!pw) return;
    setBusy(p.id);
    try {
      await post(`/api/admin/purchases/${p.id}/refund`, undefined, { headers: { "X-Confirm-Password": pw } });
    } catch (e) {
      toast.err(e);
      setBusy(null);
      return;
    }
    toast.ok(t.t("Refund initiated. Confirming with Stripe…"));
    // The refund lands with Stripe's charge.refunded webhook; poll the
    // row for up to 30 s so the operator gets a clear answer.
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
        const r = await get<Purchase>(`/api/admin/purchases/${p.id}`);
        if (r.state === "refunded") {
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

  const statePill = (p: Purchase) => {
    // Invoice rows are "paid" in the database from fulfilment, but the
    // org pays later, off-platform; say what is really the case.
    if (p.provider === "invoice" && p.state === "paid") {
      if (p.voided_at) return <Pill tone="bad">{t.t("void")}</Pill>;
      if (p.billed_at) return <Pill tone="good">{t.t("billed")}</Pill>;
      return <Pill tone="warn">{t.t("to invoice")}</Pill>;
    }
    const tone: Tone = p.state === "paid" ? "good" : p.state === "pending" ? "warn" : p.state === "refunded" ? "bad" : "neutral";
    return <Pill tone={tone}>{t.t(p.state)}</Pill>;
  };

  const actions = (p: Purchase) => {
    const out = [];
    if (p.invoice_url) {
      out.push(<a key="r" className="btn sm ghost" href={p.invoice_url} target="_blank" rel="noopener">{t.t("Receipt")}</a>);
    }
    if (isAdmin && p.provider === "invoice" && p.state === "paid" && !p.voided_at) {
      const next = !p.billed_at;
      out.push(
        <Button key="b" size="sm" disabled={busy === p.id} onClick={() => void act(p, () => post(`/api/admin/purchases/${p.id}/billed`, { billed: next }), next ? t.t("Marked billed") : t.t("Marked unbilled"))}>
          {p.billed_at ? t.t("Unmark billed") : t.t("Mark billed")}
        </Button>,
      );
      if (!p.billed_at) {
        out.push(
          <Button
            key="v"
            size="sm"
            disabled={busy === p.id}
            onClick={async () => {
              const yes = await dialogs.confirm({
                title: t.t("Void"),
                message: t.t("Void this purchase so it won't appear on the next invoice? The players stay on the pass; only the billable line is removed."),
                confirmLabel: t.t("Void"),
                danger: true,
              });
              if (yes) void act(p, () => post(`/api/admin/purchases/${p.id}/void`), t.t("Voided"));
            }}
          >
            {t.t("Void")}
          </Button>,
        );
      }
    }
    if (isAdmin && p.state === "paid" && p.provider === "stripe") {
      out.push(
        p.pre_migration ? (
          <Pill key="l" title={t.t("This purchase was imported from a previous zv deployment. Refund it through the pre-migration Stripe dashboard, not this button.")}>
            {t.t("Refund via legacy Stripe dashboard")}
          </Pill>
        ) : (
          <Button key="f" size="sm" loading={busy === p.id} onClick={() => void refund(p)}>{t.t("Refund")}</Button>
        ),
      );
    }
    return out.length ? <Row gap="sm" end>{out}</Row> : null;
  };

  const empty = (
    <Empty
      title={t.t("No purchases yet.")}
      illustration={SPOTS.noPurchases}
      action={<a className="btn primary" href={href({ name: CATALOG_ENABLED ? "catalog" : "adventures" })}><Coins size={ICON.md} aria-hidden />{CATALOG_ENABLED ? t.t("Open the store") : t.t("Open your adventures")}</a>}
    >
      {CATALOG_ENABLED ? t.t("Browse the catalog and get an event pass to start scheduling events.") : t.t("Get an event pass from an adventure's page to start scheduling events.")}
    </Empty>
  );

  // A filter nothing matches is left out, unless it is the one chosen.
  const filters = ([
    { key: "all", label: t.t("All") },
    { key: "tobill", label: t.t("To bill") },
    { key: "invoice", label: t.t("Invoice") },
    { key: "stripe", label: t.t("Stripe") },
    { key: "free", label: t.t("Free") },
  ] as { key: Filter; label: string }[]).filter((f) => f.key === filter || f.key === "all" || purchases.some((p) => matches(p, f.key)));
  const total = shown.reduce((acc, p) => acc + (p.amount_cents || 0), 0);
  const orgs = new Set(shown.map((p) => p.org_id)).size;

  return (
    <Card
      title={t.t("Purchases")}
      subtitle={
        isAdmin
          ? t.t('Every purchase across all orgs. Off-platform invoices show under "To bill" until you mark them billed.')
          : t.t("Every event-pass purchase. Paid ones give an event pass you can schedule from the Adventures page.")
      }
      actions={
        <>
          {hasStripePaid && <Button size="sm" loading={busy === "portal"} onClick={() => void portal()}>{t.t("Update payment method")}</Button>}
          {purchases.length > 0 && <a className="btn sm" href={href({ name: "adventures" })}>{CATALOG_ENABLED ? t.t("Browse catalog") : t.t("Open your adventures")}</a>}
        </>
      }
      flush
    >
      {isAdmin && purchases.length > 0 && (
        <div style={{ padding: "0 var(--dz-space-4)", display: "flex", flexDirection: "column", gap: "var(--dz-space-2)" }}>
          <div className="cs-row cs-row--sm" role="group" aria-label={t.t("Filter")}>
            {filters.map((f) => (
              <Button key={f.key} size="sm" variant={filter === f.key ? "primary" : "ghost"} aria-pressed={filter === f.key} onClick={() => setFilter(f.key)}>{f.label}</Button>
            ))}
          </div>
          {shown.length > 0 && (
            <p style={{ margin: 0, color: "var(--dz-muted)", fontSize: "var(--dz-text-sm)" }}>
              {t.plural(shown.length, { one: t.t("{{n}} purchase"), other: t.t("{{n}} purchases") })}
              {" · "}
              {orgs === 1 ? t.t("{{total}} from one org", { total: fmtMoney(total, "SEK", locale) }) : t.t("{{total}} across {{orgs}} orgs", { total: fmtMoney(total, "SEK", locale), orgs })}
            </p>
          )}
        </div>
      )}
      {purchases.length === 0 ? (
        <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}>{empty}</div>
      ) : shown.length === 0 ? (
        <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><Empty title={t.t("Nothing matches this filter.")} /></div>
      ) : (
        <Table
          caption={t.t("Purchases")}
          rows={shown}
          rowKey={(p) => p.id}
          columns={[
            {
              key: "adv",
              header: t.t("Adventure"),
              cell: (p) => (
                <div className="acct-item__text">
                  <strong>{p.adventure_title || p.adventure_slug}</strong>
                  <span className="acct-item__meta" title={fmtDateTime(p.created_at, locale)}>{fmtDate(p.created_at, locale)}</span>
                </div>
              ),
            },
            ...(isAdmin ? [{ key: "org", header: t.t("Org"), wide: true, cell: (p: Purchase) => "#" + p.org_id }] : []),
            { key: "players", header: t.t("Players"), align: "end" as const, cell: (p) => p.max_players },
            { key: "total", header: t.t("Total"), align: "end" as const, cell: (p) => money(p) },
            { key: "state", header: t.t("State"), cell: statePill },
            ...(shown.some((p) => actions(p)) ? [{ key: "actions", header: <span className="cs-sr">{t.t("Actions")}</span>, align: "end" as const, cell: actions }] : []),
          ]}
        />
      )}
    </Card>
  );
}
