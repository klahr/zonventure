import { useState } from "react";
import { get, put } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, ErrorBox, Field, Loading, Pill, Stat, Stats, fmtMoney, useLoad, useToast } from "../../ui/index.ts";
import type { Account } from "../settings/teams-billing.tsx";

interface OrgBilling {
  account: Account;
  prices: { currency: string };
}

const url = (orgId: number) => `/api/admin/organizations/${orgId}/billing`;

/**
 * An org's balance for its teams, for the platform admin: credit or
 * charge it with a note, or set its monthly credit limit
 * (admin/billing.go SetOrgBilling).
 */
export function OrgBillingCard({ orgId, readOnly }: { orgId: number; readOnly: boolean }) {
  const t = useT();
  const loc = t.current();
  const load = useLoad((signal) => get<OrgBilling>(url(orgId), { signal }), [orgId]);
  const [open, setOpen] = useState<null | "adjust" | "limit">(null);
  const b = load.data;
  const money = (c: number) => fmtMoney(c, b?.prices.currency, loc);
  return (
    <Card
      title={t.t("Team billing")}
      actions={!readOnly && b && (
        <>
          <Button size="sm" onClick={() => setOpen("adjust")}>{t.t("Adjust balance")}</Button>
          {b.account.mode === "monthly" && <Button size="sm" onClick={() => setOpen("limit")}>{t.t("Credit limit")}</Button>}
        </>
      )}
    >
      {load.error && !b ? <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
        : !b ? <Loading label={t.t("Loading…")} />
        : (
          <Stats>
            <Stat value={money(b.account.balance_cents)} label={t.t("Balance")} {...(b.account.balance_cents < 0 ? { tone: "warn" as const } : {})} />
            <Stat value={money(-b.account.floor_cents)} label={b.account.mode === "monthly" ? t.t("Credit") : t.t("Overdraft")} />
            <Stat value={b.account.mode === "monthly" ? t.t("Monthly") : t.t("Prepaid")} label={b.account.card || (b.account.invoice ? t.t("Invoice") : t.t("No card"))} />
            {b.account.overdue && <Pill tone="bad">{t.t("Overdue")}</Pill>}
          </Stats>
        )}
      {open === "adjust" && b && <AdjustDialog orgId={orgId} currency={b.prices.currency} onClose={() => setOpen(null)} onDone={() => { setOpen(null); load.reload(); }} />}
      {open === "limit" && b && <LimitDialog orgId={orgId} account={b.account} currency={b.prices.currency} onClose={() => setOpen(null)} onDone={() => { setOpen(null); load.reload(); }} />}
    </Card>
  );
}

const cents = (s: string) => Math.round((Number.parseFloat(s) || 0) * 100);

function AdjustDialog({ orgId, currency, onClose, onDone }: { orgId: number; currency: string; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      await put(url(orgId), { adjust_cents: cents(amount), note: note.trim() });
      toast.ok(t.t("Balance adjusted"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Adjust balance")}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy} disabled={cents(amount) === 0 || !note.trim()}>{t.t("Save")}</Button></>}
    >
      <Field label={t.t("Amount ({{currency}})", { currency })} hint={t.t("Positive credits the organisation, negative charges it.")}>
        <input type="number" step={0.01} inputMode="decimal" required autoFocus value={amount} onChange={(e) => setAmount(e.target.value)} />
      </Field>
      <Field label={t.t("Note")} hint={t.t("Why, for the record.")}>
        <input required maxLength={200} value={note} onChange={(e) => setNote(e.target.value)} />
      </Field>
    </Dialog>
  );
}

function LimitDialog({ orgId, account, currency, onClose, onDone }: { orgId: number; account: Account; currency: string; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const [limit, setLimit] = useState(String(account.credit_limit_cents / 100));
  const [busy, setBusy] = useState(false);
  const save = async (body: object) => {
    setBusy(true);
    try {
      await put(url(orgId), body);
      toast.ok(t.t("Credit limit saved"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Credit limit")}
      onClose={onClose}
      onSubmit={() => void save({ credit_limit_cents: cents(limit) })}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button disabled={busy} onClick={() => void save({ clear_credit_limit: true })}>{t.t("Use the platform's")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>
        </>
      }
    >
      <Field label={t.t("Credit limit ({{currency}})", { currency })} hint={t.t("How far a monthly organisation's teams may run before its bill.")}>
        <input type="number" min={0} step={1} inputMode="decimal" required autoFocus value={limit} onChange={(e) => setLimit(e.target.value)} />
      </Field>
    </Dialog>
  );
}
