import { useState } from "react";
import { Ban, Download, Ticket } from "lucide-react";
import { ApiError, get, post } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { Button, Callout, Card, ErrorBox, Field, ICON, Loading, Row, errorText, fmtDate, fmtMoney, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import type { EventInfo } from "./api.ts";

/** voucherOfferDTO, internal/admin/voucher_batches.go. */
interface VoucherOffer {
  allowed: boolean;
  price_cents: number;
  currency: string;
  vat_excluded: boolean;
  default_team_size: number;
  batches: VoucherBatch[];
}

interface VoucherBatch {
  id: number;
  label: string;
  team_size: number;
  codes: number;
  used: number;
  created_at: string;
  voided: boolean;
}

/** The most codes one batch makes (MaxVoucherBatch). */
const MAX_BATCH = 2000;

/**
 * Pay-later voucher codes for a self-guided event: printed in an
 * envelope or on a gift card and sold anywhere, a bookstore say. A
 * buyer types one at /play and its first use starts a team; each code
 * used is on the organisation's monthly invoice, unused ones cost
 * nothing. Organisations with invoice billing only.
 */
export function VoucherCodesCard({ event }: { event: EventInfo }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const canEdit = user?.role === "org_admin" || user?.role === "admin";
  const base = `/api/admin/events/${event.id}/voucher-batches`;
  const load = useLoad((signal) => get<VoucherOffer>(base, { signal }), [event.id]);
  const [count, setCount] = useState("100");
  const [size, setSize] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  if (load.error && !load.data) return <Card title={t.t("Voucher codes")}><ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} /></Card>;
  const d = load.data;
  const price = d ? fmtMoney(d.price_cents, d.currency, t.current()) : "";
  const vat = d?.vat_excluded ? t.t("excl. VAT") : t.t("incl. VAT");
  const playURL = `${(user?.public_url || location.origin).replace(/\/$/, "")}/play`;

  const create = async () => {
    if (!d) return;
    const n = Number.parseInt(count, 10);
    const teamSize = size.trim() === "" ? d.default_team_size : Number.parseInt(size, 10);
    if (!Number.isInteger(n) || n < 1 || n > MAX_BATCH) {
      setErr(t.t("Make between 1 and {{max}} codes at a time.", { max: String(MAX_BATCH) }));
      return;
    }
    if (!Number.isInteger(teamSize) || teamSize < 1 || teamSize > 20) {
      setErr(t.t("A team is 1 to 20 players."));
      return;
    }
    setErr("");
    const ok = await dialogs.confirm({
      title: t.t("Make {{n}} codes?", { n: String(n) }),
      message: d.price_cents > 0
        ? t.t("You pay {{price}} {{vat}} for each code that is used, on your monthly invoice: at most {{total}} if all are used. Unused codes cost nothing.", { price, vat, total: fmtMoney(d.price_cents * n, d.currency, t.current()) })
        : t.t("Using these codes is free right now."),
      confirmLabel: t.t("Make codes"),
    });
    if (!ok) return;
    setBusy(true);
    try {
      load.set(await post<VoucherOffer>(base, { count: n, team_size: teamSize, label: label.trim() }));
      setLabel("");
      toast.ok(t.t("{{n}} codes made. Download them for printing.", { n: String(n) }));
    } catch (e) {
      const reason = e instanceof ApiError ? (e.body as { reason?: string } | undefined)?.reason : undefined;
      setErr(reason === "not_invoice" ? t.t("Voucher codes you pay later need invoice billing. Ask us to turn it on.") : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const voidBatch = async (b: VoucherBatch) => {
    const ok = await dialogs.confirm({
      title: t.t("Void these codes?"),
      message: t.t("The {{n}} unused codes in “{{label}}” will stop working, say for a print run that was never sold. Teams already playing keep playing, and codes already used stay on your invoice.", { n: String(b.codes - b.used), label: b.label || t.t("Codes from {{date}}", { date: fmtDate(b.created_at, t.current()) }) }),
      confirmLabel: t.t("Void codes"),
      danger: true,
    });
    if (!ok) return;
    try {
      load.set(await post<VoucherOffer>(`/api/admin/voucher-batches/${b.id}/void`, {}));
      toast.ok(t.t("Codes voided"));
    } catch (e) {
      toast.err(errorText(e));
    }
  };

  return (
    <Card
      title={<span className="vc-title"><Ticket size={ICON.lg} aria-hidden /> {t.t("Voucher codes")}</span>}
      subtitle={t.t("Print a code in each envelope or on each gift card and sell them anywhere, a bookstore say. Buyers type it at {{url}} to start; one code starts one team, and teammates use the same code.", { url: playURL })}
    >
      {!d ? (
        <Loading label={t.t("Loading…")} />
      ) : !d.allowed ? (
        <Callout variant="info">{t.t("Voucher codes you pay later need invoice billing. Ask us to turn it on, and you can print codes here and pay for those used on a monthly invoice.")}</Callout>
      ) : (
        <div className="vc">
          <p className="vc-price">
            {d.price_cents > 0
              ? t.t("{{price}} {{vat}} per code used, on your monthly invoice. Unused codes cost nothing.", { price, vat })
              : t.t("Using codes is free right now.")}
          </p>
          {canEdit && (
            <form className="vc-form" onSubmit={(e) => { e.preventDefault(); void create(); }}>
              <Row>
                <Field label={t.t("Codes")}>
                  <input className="st-num" inputMode="numeric" value={count} onChange={(e) => setCount(e.target.value)} />
                </Field>
                <Field label={t.t("Players per code")}>
                  <input className="st-num" inputMode="numeric" value={size} placeholder={String(d.default_team_size)} onChange={(e) => setSize(e.target.value)} />
                </Field>
                <Field label={t.t("Label (optional)")}>
                  <input value={label} maxLength={100} placeholder={t.t("Bookstores, spring print run")} onChange={(e) => setLabel(e.target.value)} />
                </Field>
              </Row>
              {err && <p className="vc-err" role="alert">{err}</p>}
              <div><Button type="submit" variant="primary" loading={busy}>{t.t("Make codes")}</Button></div>
            </form>
          )}
          {d.batches.length > 0 ? (
            <ul className="vc-batches">
              {d.batches.map((b) => (
                <li key={b.id} className={b.voided ? "is-voided" : ""}>
                  <div className="vc-batch__head">
                    <strong>{b.label || t.t("Codes from {{date}}", { date: fmtDate(b.created_at, t.current()) })}</strong>
                    <span className="vc-batch__meta">
                      {t.t("{{n}} players per code", { n: String(b.team_size) })} · {fmtDate(b.created_at, t.current())}
                      {b.voided && <> · {t.t("Voided")}</>}
                    </span>
                  </div>
                  <div className="vc-batch__use" aria-label={t.t("{{used}} of {{codes}} used", { used: String(b.used), codes: String(b.codes) })}>
                    <span className="vc-bar"><span style={{ width: `${b.codes ? Math.round((100 * b.used) / b.codes) : 0}%` }} /></span>
                    <span>{t.t("{{used}} of {{codes}} used", { used: String(b.used), codes: String(b.codes) })}</span>
                  </div>
                  <div className="vc-batch__actions">
                    <a className="vc-link" href={`/api/admin/voucher-batches/${b.id}/codes.csv`} download>
                      <Download size={ICON.sm} aria-hidden /> {t.t("Download codes (CSV)")}
                    </a>
                    {canEdit && !b.voided && b.used < b.codes && (
                      <Button variant="ghost" size="sm" icon={<Ban size={ICON.sm} />} onClick={() => void voidBatch(b)}>{t.t("Void unused")}</Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="vc-muted">{t.t("No codes yet. Make a batch, download it and send it to your print shop.")}</p>
          )}
        </div>
      )}
    </Card>
  );
}
