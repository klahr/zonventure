import { useEffect, useState } from "react";
import { CreditCard, Plus, Trash2 } from "lucide-react";
import { get, post, put, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import {
  Button, Callout, Card, Dialog, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Stat, Stats, Table, fmtDate, fmtMoney, useDialogs, useLoad, useToast, type Column, type Tone,
} from "../../ui/index.ts";

/** billing.Account, internal/billing. */
export interface Account {
  mode: "prepaid" | "monthly";
  balance_cents: number;
  floor_cents: number;
  credit_limit_cents: number;
  card: string;
  invoice: boolean;
  overdue: boolean;
}

/** billingBillDTO, internal/admin/billing.go. */
interface Bill {
  id: number;
  org_id?: number;
  org_name?: string;
  period: string;
  method: "card" | "invoice";
  amount_cents: number;
  currency: string;
  state: "open" | "paid" | "failed" | "void";
  attempts: number;
  last_error?: string;
  invoice_number?: string;
  created_at: string;
  paid_at?: string;
}

/** billingDTO, internal/admin/billing.go. */
interface Billing {
  account: Account;
  prices: { team_cents: number; team_phones: number; phone_cents: number; currency: string; vat_excluded: boolean };
  top_ups: { amount_cents: number; bonus_cents: number }[];
  month: { teams: number; phones: number; cents: number };
  events: { event_id?: number; name?: string; teams: number; phones: number; cents: number }[];
  bills: Bill[];
  bought: { amount_cents: number; bonus_cents: number; state: string; created_at: string }[];
  payments: boolean;
  cards: boolean;
}

const BILL_TONE: Record<Bill["state"], Tone> = { open: "warn", paid: "good", failed: "bad", void: "neutral" };
const BILL_LABEL: Record<Bill["state"], string> = { open: "Open", paid: "Paid", failed: "Failed", void: "Void" };

// Checkout sends the browser back with ?topped_up=1 or ?card=saved: say
// so once, then drop it from the URL.
function takeReturnFlag(): string | null {
  const q = new URLSearchParams(location.search);
  const flag = q.has("topped_up") ? "topped_up" : q.get("card") === "saved" ? "card" : null;
  if (flag) history.replaceState(null, "", location.pathname + location.hash);
  return flag;
}

/**
 * Paying for teams (PLATFORM_PRICING.md): the org's balance, how it pays
 * — prepaid top-ups, or a monthly bill to a saved card or an invoice —
 * what its teams cost this month, and its bills. Org admins only.
 */
export function TeamBillingCard() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const load = useLoad((signal) => get<Billing>("/api/admin/orgs/me/billing", { signal }), []);
  const [flag] = useState(takeReturnFlag);
  const [busy, setBusy] = useState(false);
  const [topUp, setTopUp] = useState(false);
  useEffect(() => {
    if (flag === "topped_up") toast.ok(t.t("Thanks — your balance is topped up as soon as the payment is through."));
    if (flag === "card") toast.ok(t.t("Your card is saved."));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (load.error && !load.data) return <Card title={t.t("Teams")}><ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} /></Card>;
  if (!load.data) return <Card title={t.t("Teams")}><Loading label={t.t("Loading…")} /></Card>;
  const b = load.data;
  const a = b.account;
  const loc = t.current();
  const kr = (cents: number) => fmtMoney(cents, b.prices.currency, loc);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  const go = (fn: () => Promise<{ checkout_url: string }>) => run(async () => {
    location.href = (await fn()).checkout_url;
  });
  const setMode = (mode: Account["mode"]) => run(async () => load.set(await put<Billing>("/api/admin/orgs/me/billing", { mode })));
  const saveCard = () => go(() => post<{ checkout_url: string }>("/api/admin/orgs/me/billing/card"));
  const removeCard = async () => {
    if (!(await dialogs.confirm({ title: t.t("Remove the card?"), message: t.t("Monthly bills can't be charged without a card: teams then need a prepaid balance."), confirmLabel: t.t("Remove"), danger: true }))) return;
    await run(async () => load.set(await request<Billing>("/api/admin/orgs/me/billing/card", { method: "DELETE" })));
  };
  const payNow = () => run(async () => {
    load.set(await post<Billing>("/api/admin/orgs/me/billing/pay"));
    toast.ok(t.t("Charged"));
  });
  const available = Math.max(0, a.balance_cents - a.floor_cents);
  const teamsLeft = b.prices.team_cents > 0 ? Math.floor(available / b.prices.team_cents) : null;
  const unpaid = b.bills.filter((x) => x.state === "open" || x.state === "failed");

  const columns: Column<Billing["events"][number]>[] = [
    { key: "name", header: t.t("Event"), cell: (e) => (e.event_id ? <a href={href({ name: "event", id: e.event_id })}>{e.name || `#${e.event_id}`}</a> : t.t("(deleted)")) },
    { key: "teams", header: t.t("Teams"), align: "end", cell: (e) => e.teams },
    { key: "phones", header: t.t("Extra phones"), align: "end", cell: (e) => e.phones },
    { key: "cents", header: t.t("Cost"), align: "end", cell: (e) => kr(e.cents) },
  ];
  const billColumns: Column<Bill>[] = [
    { key: "period", header: t.t("Month"), cell: (x) => x.period },
    { key: "amount", header: t.t("Amount"), align: "end", cell: (x) => kr(x.amount_cents) },
    { key: "method", header: t.t("Paid by"), cell: (x) => (x.method === "card" ? t.t("Card") : t.t("Invoice")) },
    { key: "state", header: t.t("State"), cell: (x) => <Pill tone={BILL_TONE[x.state]} {...(x.last_error ? { title: x.last_error } : {})}>{t.t(BILL_LABEL[x.state])}</Pill> },
  ];

  return (
    <>
      <Card
        title={<>{t.t("Teams")} <HelpPopover label={t.t("About paying for teams")} body={t.t("Every team that plays one of your events costs {{team}}, its first {{phones}} phones included, and {{phone}} for each phone after: charged as its first phone joins. A team bought as a ticket in your store paid in the sale. Everything else comes off this balance.", { team: kr(b.prices.team_cents), phones: b.prices.team_phones, phone: kr(b.prices.phone_cents) })} /></>}
        subtitle={a.mode === "monthly" ? t.t("Billed monthly for last month's teams.") : t.t("Prepaid: top up your balance before your teams play.")}
      >
        {a.overdue && (
          <Callout variant="warning">
            {t.t("A monthly bill couldn't be charged. New teams can't start until it is paid.")}
            {a.card && <> <Button size="sm" variant="primary" loading={busy} onClick={() => void payNow()}>{t.t("Pay now")}</Button></>}
          </Callout>
        )}
        {!a.overdue && available <= 0 && (
          <Callout variant="warning">{a.mode === "monthly" ? t.t("You've reached your credit limit: new teams can't start until your bill is paid.") : t.t("Your balance is used up: new teams can't start until you top up.")}</Callout>
        )}
        <Stats>
          <Stat label={t.t("Balance")} value={kr(a.balance_cents)} {...(a.balance_cents < 0 ? { tone: "warn" as const } : {})} />
          {a.mode === "monthly" && a.credit_limit_cents > 0 && <Stat label={t.t("Credit limit")} value={kr(a.credit_limit_cents)} />}
          {teamsLeft !== null && <Stat label={t.t("Room for")} value={t.plural(teamsLeft, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") })} />}
          <Stat label={t.t("This month")} value={`${t.plural(b.month.teams, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") })} · ${kr(b.month.cents)}`} />
        </Stats>
        <div className="st-billing__actions">
          {a.mode === "prepaid" && (
            <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} disabled={!b.payments || busy} onClick={() => setTopUp(true)}>{t.t("Top up")}</Button>
          )}
          {b.cards && (a.card
            ? <span className="st-billing__card"><CreditCard size={ICON.md} aria-hidden /> {a.card}
                <Button size="sm" variant="ghost" onClick={() => void saveCard()} disabled={busy}>{t.t("Change")}</Button>
                <Button size="icon" variant="ghost" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Remove the card")} title={t.t("Remove the card")} disabled={busy} onClick={() => void removeCard()} /></span>
            : <Button icon={<CreditCard size={ICON.md} aria-hidden />} disabled={busy} onClick={() => void saveCard()}>{t.t("Save a card")}</Button>)}
        </div>
        <fieldset className="st-billing__mode" disabled={busy}>
          <legend>{t.t("How you pay")}</legend>
          <label className="cs-row cs-row--sm">
            <input type="radio" name="billing-mode" checked={a.mode === "prepaid"} onChange={() => void setMode("prepaid")} />
            <span><strong>{t.t("Prepaid")}</strong> — {t.t("top up by card; bigger top-ups give a bonus.")}</span>
          </label>
          <label className="cs-row cs-row--sm">
            <input type="radio" name="billing-mode" checked={a.mode === "monthly"} disabled={!a.card && !a.invoice} onChange={() => void setMode("monthly")} />
            <span><strong>{t.t("Monthly")}</strong> — {a.invoice ? t.t("billed on the 1st for last month's teams, by invoice or to your card.") : t.t("billed on the 1st for last month's teams, to your saved card.")}
              {!a.card && !a.invoice && <> {t.t("Save a card first.")}</>}</span>
          </label>
        </fieldset>
      </Card>
      {b.events.length > 0 && (
        <Card title={t.t("This month's teams")} flush>
          <Table columns={columns} rows={b.events} rowKey={(e) => String(e.event_id ?? 0)} />
        </Card>
      )}
      {b.bills.length > 0 && (
        <Card title={t.t("Bills")} flush actions={unpaid.length > 0 && a.card ? <Button size="sm" loading={busy} onClick={() => void payNow()}>{t.t("Pay now")}</Button> : undefined}>
          <Table columns={billColumns} rows={b.bills} rowKey={(x) => String(x.id)} />
        </Card>
      )}
      {topUp && (
        <TopUpDialog
          options={b.top_ups}
          format={kr}
          onClose={() => setTopUp(false)}
          onPick={(amount) => go(() => post<{ checkout_url: string }>("/api/admin/orgs/me/billing/top-up", { amount_cents: amount }))}
          busy={busy}
          teamCents={b.prices.team_cents}
        />
      )}
    </>
  );
}

/** Picking a top-up: each amount with its bonus and what a team then costs. */
function TopUpDialog(props: {
  options: Billing["top_ups"];
  format: (cents: number) => string;
  onClose: () => void;
  onPick: (amount: number) => void;
  busy: boolean;
  teamCents: number;
}) {
  const t = useT();
  const [amount, setAmount] = useState(props.options[0]?.amount_cents ?? 0);
  return (
    <Dialog
      title={t.t("Top up your balance")}
      onClose={props.onClose}
      footer={
        <>
          <Button variant="ghost" onClick={props.onClose}>{t.t("Cancel")}</Button>
          <Button variant="primary" loading={props.busy} disabled={!amount} onClick={() => props.onPick(amount)}>{t.t("Pay {{amount}}", { amount: props.format(amount) })}</Button>
        </>
      }
    >
      <div className="st-topup-pick" role="radiogroup">
        {props.options.map((o) => {
          const total = o.amount_cents + o.bonus_cents;
          const per = props.teamCents > 0 ? Math.round((props.teamCents * o.amount_cents) / total) : 0;
          return (
            <label key={o.amount_cents} className={"st-topup-pick__item" + (amount === o.amount_cents ? " is-on" : "")}>
              <input type="radio" name="top-up" checked={amount === o.amount_cents} onChange={() => setAmount(o.amount_cents)} />
              <strong>{props.format(o.amount_cents)}</strong>
              <span>{o.bonus_cents > 0 ? t.t("you get {{total}}", { total: props.format(total) }) : t.t("no bonus")}</span>
              {per > 0 && <small>{t.t("a team then costs {{price}}", { price: props.format(per) })}</small>}
            </label>
          );
        })}
      </div>
      <p className="cs-field__hint st-flush">{t.t("Paid by card. Top-ups don't expire and aren't refunded. You'll get a receipt by email.")}</p>
    </Dialog>
  );
}

/**
 * The platform admin's monthly round: every organisation's bills —
 * invoices to send and mark paid once they are, cards that failed.
 */
export function BillsCard() {
  const t = useT();
  const toast = useToast();
  const [state, setState] = useState("open");
  const load = useLoad((signal) => get<{ bills: Bill[] }>(`/api/admin/billing/bills?state=${encodeURIComponent(state)}`, { signal }), [state]);
  const [marking, setMarking] = useState<Bill | null>(null);
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);
  const loc = t.current();
  const mark = async () => {
    if (!marking) return;
    setBusy(true);
    try {
      await post(`/api/admin/billing/bills/${marking.id}/paid`, { invoice_number: number.trim() });
      toast.ok(t.t("Marked paid"));
      setMarking(null);
      setNumber("");
      load.reload();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  const columns: Column<Bill>[] = [
    { key: "org", header: t.t("Organization"), cell: (x) => (x.org_id ? <a href={href({ name: "organization", id: x.org_id })}>{x.org_name}</a> : x.org_name) },
    { key: "period", header: t.t("Month"), cell: (x) => x.period },
    { key: "amount", header: t.t("Amount"), align: "end", cell: (x) => fmtMoney(x.amount_cents, x.currency, loc) },
    { key: "method", header: t.t("Paid by"), cell: (x) => (x.method === "card" ? t.t("Card") : t.t("Invoice")) },
    { key: "state", header: t.t("State"), cell: (x) => <Pill tone={BILL_TONE[x.state]} {...(x.last_error ? { title: x.last_error } : {})}>{t.t(BILL_LABEL[x.state])}</Pill> },
    { key: "when", header: t.t("Billed"), cell: (x) => fmtDate(x.created_at, loc) },
    { key: "act", header: "", align: "end", cell: (x) => (x.state !== "paid" ? <Button size="sm" onClick={() => setMarking(x)}>{t.t("Mark paid")}</Button> : x.invoice_number ?? "") },
  ];
  return (
    <Card
      title={t.t("Monthly bills")}
      subtitle={t.t("Organizations billed monthly for their teams. Card bills are charged on the 1st and retried for a week; invoices you send yourself, then mark paid here.")}
      flush
      actions={
        <select value={state} onChange={(e) => setState(e.target.value)} aria-label={t.t("Bills")}>
          <option value="open">{t.t("Open")}</option>
          <option value="failed">{t.t("Failed")}</option>
          <option value="paid">{t.t("Paid")}</option>
          <option value="">{t.t("All")}</option>
        </select>
      }
    >
      {load.error && !load.data ? <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
        : !load.data ? <Loading label={t.t("Loading…")} />
        : load.data.bills.length === 0 ? <p className="cs-field__hint st-flush">{t.t("No bills here.")}</p>
        : <Table columns={columns} rows={load.data.bills} rowKey={(x) => String(x.id)} />}
      {marking && (
        <Dialog
          title={t.t("Mark {{org}}'s bill for {{month}} paid", { org: marking.org_name ?? "", month: marking.period })}
          onClose={() => setMarking(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setMarking(null)}>{t.t("Cancel")}</Button>
              <Button variant="primary" loading={busy} onClick={() => void mark()}>{t.t("Mark paid")}</Button>
            </>
          }
        >
          <Field label={t.t("Invoice number")} hint={t.t("From your accounting system, optional.")}>
            <input value={number} onChange={(e) => setNumber(e.target.value)} maxLength={64} />
          </Field>
        </Dialog>
      )}
    </Card>
  );
}
