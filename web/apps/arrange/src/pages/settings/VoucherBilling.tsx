import { useState } from "react";
import { get, post } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, ErrorBox, Field, Loading, Table, errorText, fmtDate, fmtMoney, useLoad, useToast, type Column } from "../../ui/index.ts";

/** voucherUseDTO, internal/admin/voucher_batches.go. */
interface VoucherUse {
  org_id: number;
  org_name?: string;
  codes: number;
  amount_cents: number;
  currency: string;
  since?: string;
}

/**
 * What the organisation owes for pay-later voucher codes its buyers
 * have used since the last invoice. Shown only once something is owed.
 */
export function VoucherUseCard() {
  const t = useT();
  const load = useLoad((signal) => get<VoucherUse>("/api/admin/orgs/me/voucher-use", { signal }), []);
  const u = load.data;
  if (!u || u.codes === 0) return null;
  return (
    <Card title={t.t("Voucher codes")} subtitle={t.t("Codes your buyers have used, on your next invoice from the platform.")}>
      <p className="st-flush">
        <strong>{t.t("{{n}} used, {{amount}}", { n: String(u.codes), amount: fmtMoney(u.amount_cents, u.currency, t.current()) })}</strong>
        {u.since && <> · {t.t("since {{date}}", { date: fmtDate(u.since, t.current()) })}</>}
      </p>
    </Card>
  );
}

/**
 * The platform admin's monthly round: every organisation with used
 * voucher codes not yet invoiced, and marking them invoiced with the
 * invoice's number once it is sent from the accounting system.
 */
export function VoucherBillingCard() {
  const t = useT();
  const toast = useToast();
  const load = useLoad((signal) => get<{ orgs: VoucherUse[] }>("/api/admin/voucher-billing", { signal }), []);
  const [open, setOpen] = useState<VoucherUse | null>(null);
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);

  const mark = async () => {
    if (!open) return;
    setBusy(true);
    try {
      const r = await post<{ billed: number }>(`/api/admin/organizations/${open.org_id}/voucher-billing`, { invoice_number: number.trim() });
      toast.ok(t.t("{{n}} codes marked invoiced", { n: String(r.billed) }));
      setOpen(null);
      setNumber("");
      load.reload();
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const columns: Column<VoucherUse>[] = [
    { key: "org", header: t.t("Organization"), cell: (u) => u.org_name ?? "" },
    { key: "codes", header: t.t("Codes used"), cell: (u) => String(u.codes) },
    { key: "amount", header: t.t("To invoice"), cell: (u) => fmtMoney(u.amount_cents, u.currency, t.current()) },
    { key: "since", header: t.t("Since"), cell: (u) => (u.since ? fmtDate(u.since, t.current()) : "") },
    { key: "act", header: "", cell: (u) => <Button size="sm" onClick={() => { setOpen(u); setNumber(""); }}>{t.t("Mark invoiced")}</Button> },
  ];
  return (
    <Card title={t.t("Voucher codes to invoice")} subtitle={t.t("Pay-later voucher codes used since the last invoice, per organization. Send the invoice from your accounting system, then mark it here.")} flush={!!load.data?.orgs.length}>
      {load.error && !load.data ? (
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      ) : !load.data ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <Table columns={columns} rows={load.data.orgs} rowKey={(u) => u.org_id} empty={t.t("Nothing to invoice.")} />
      )}
      {open && (
        <Dialog
          title={t.t("Mark invoiced: {{org}}", { org: open.org_name ?? "" })}
          onClose={() => setOpen(null)}
          onSubmit={() => void mark()}
          footer={
            <>
              <Button variant="ghost" onClick={() => setOpen(null)}>{t.t("Cancel")}</Button>
              <Button type="submit" variant="primary" loading={busy} disabled={!number.trim()}>{t.t("Mark invoiced")}</Button>
            </>
          }
        >
          <p className="st-intro">{t.t("{{n}} codes, {{amount}}. Every code used up to now goes on this invoice.", { n: String(open.codes), amount: fmtMoney(open.amount_cents, open.currency, t.current()) })}</p>
          <Field label={t.t("Invoice number")}>
            <input value={number} maxLength={100} autoFocus onChange={(e) => setNumber(e.target.value)} />
          </Field>
        </Dialog>
      )}
    </Card>
  );
}
