import { useEffect, useState, type ReactNode } from "react";
import { get, put } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, ErrorBox, Field, Loading, Row, fmtMoney, platformFeeOn, useDialogs, useLoad, useToast } from "../../ui/index.ts";

/** pricingResponse, internal/admin/pricing.go. Amounts in öre. */
interface Pricing {
  player_cents: number;
  free_players: number;
  hosted_block_cents: number;
  fee_percent_bps: number;
  fee_fixed_cents: number;
  fee_min_cents: number;
  vat_excluded: boolean;
  payout_terms: string;
}

/** unconnectedSeller, internal/admin/pricing.go. */
interface Seller {
  id: number;
  name: string;
  listings: number;
}

type Price = "player_cents" | "free_players" | "hosted_block_cents" | "fee_percent_bps" | "fee_fixed_cents" | "fee_min_cents";

/** What the fields hold while typed: kronor and percent as written. */
type Draft = Record<Price, string>;

const KRONOR: Price[] = ["player_cents", "hosted_block_cents", "fee_fixed_cents", "fee_min_cents"];

const FIELDS: Price[] = ["player_cents", "free_players", "hosted_block_cents", "fee_percent_bps", "fee_fixed_cents", "fee_min_cents"];

/** The settings alone: the server's response also carries its currency and the unconnected sellers, which are no field here. */
function pick(p: Pricing): Pricing {
  const out = { vat_excluded: !!p.vat_excluded, payout_terms: p.payout_terms ?? "" } as Pricing;
  for (const k of FIELDS) out[k] = p[k];
  return out;
}

function toDraft(p: Pricing): Draft {
  const d = {} as Draft;
  for (const k of FIELDS) {
    d[k] = KRONOR.includes(k) ? String(p[k] / 100) : k === "fee_percent_bps" ? String(p[k] / 100) : String(p[k]);
  }
  return d;
}

/** A typed number, "49,50" as well as "49.50"; NaN when it is not one. */
function num(s: string): number {
  const v = s.trim().replace(",", ".");
  return v === "" ? 0 : Number(v);
}

/** The draft's fields as stored, or the first one that is not a whole, non-negative number. */
function fromDraft(d: Draft, saved: Pricing): Pricing | Price {
  const p = { ...saved };
  for (const k of FIELDS) {
    const n = num(d[k]);
    const v = KRONOR.includes(k) || k === "fee_percent_bps" ? Math.round(n * 100) : n;
    if (!Number.isFinite(n) || n < 0 || !Number.isInteger(v)) return k;
    p[k] = v;
  }
  return p;
}

/**
 * What the platform charges, for the platform admin: the event pass,
 * the hosted pass and the cut of what orgs sell. In SEK, VAT included.
 * Each card saves on its own and applies to the next checkout; nothing
 * is set in Stripe.
 */
export function PricingTab() {
  const t = useT();
  const load = useLoad((signal) => get<Pricing & { unconnected_sellers?: Seller[] }>("/api/admin/pricing", { signal }), []);
  const [draft, setDraft] = useState<Draft | null>(null);
  useEffect(() => {
    if (load.data) setDraft(toDraft(load.data));
  }, [load.data]);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data || !draft) return <Loading label={t.t("Loading…")} />;
  const saved = pick(load.data);
  const sellers = load.data.unconnected_sellers ?? [];
  const onSaved = (p: Pricing & { unconnected_sellers?: Seller[] }) => load.set(p);
  const parsed = fromDraft(draft, saved);
  const now = typeof parsed === "string" ? saved : parsed;
  const kr = (cents: number) => fmtMoney(cents, "SEK", t.current());
  const set = (k: Price) => (e: { target: { value: string } }) => setDraft({ ...draft, [k]: e.target.value });
  const input = (k: Price, suffix: string) => (
    <span className="cs-row cs-row--sm">
      <input className="st-num" inputMode="decimal" value={draft[k]} onChange={set(k)} aria-invalid={parsed === k} />
      <span>{suffix}</span>
    </span>
  );
  const krIncl = saved.vat_excluded ? t.t("kr excl. VAT") : t.t("kr incl. VAT");
  const fee = platformFeeOn(now, 19900);

  return (
    <>
      <p className="cs-field__hint st-intro">
        {saved.vat_excluded
          ? t.t("What the platform charges, in SEK excluding VAT. A saved price applies to the next checkout; purchases already made or started keep theirs.")
          : t.t("What the platform charges, in SEK including VAT. A saved price applies to the next checkout; purchases already made or started keep theirs.")}
      </p>
      <VATCard saved={saved} onSaved={onSaved} />
      <PriceCard
        title={t.t("Event pass")}
        who={t.t("Orgs pay it for a scheduled event, to the platform's account.")}
        keys={["player_cents", "free_players"]}
        saved={saved}
        draft={draft}
        parsed={parsed}
        onSaved={onSaved}
        example={t.t("40 players: {{free}} free, {{paid}} × {{price}} = {{total}}", {
          free: Math.min(40, now.free_players),
          paid: Math.max(0, 40 - now.free_players),
          price: kr(now.player_cents),
          total: kr(Math.max(0, 40 - now.free_players) * now.player_cents),
        })}
      >
        <Row>
          <Field label={t.t("Price per player")}>{input("player_cents", krIncl)}</Field>
          <Field label={t.t("Free players per event")}>{input("free_players", t.t("players"))}</Field>
        </Row>
      </PriceCard>
      <PriceCard
        title={t.t("Hosted pass")}
        who={t.t("Paid for a hosted game or a party adventure, per block of 15 teams, to the platform's account. 0 makes hosting free.")}
        keys={["hosted_block_cents"]}
        saved={saved}
        draft={draft}
        parsed={parsed}
        onSaved={onSaved}
        example={
          now.hosted_block_cents > 0
            ? t.t("1 block (15 teams): {{one}}. 2 blocks (30 teams): {{two}}.", { one: kr(now.hosted_block_cents), two: kr(2 * now.hosted_block_cents) })
            : t.t("Hosting is free.")
        }
      >
        <Field label={t.t("Price per block of 15 teams")}>{input("hosted_block_cents", krIncl)}</Field>
      </PriceCard>
      <PriceCard
        title={t.t("Platform fee")}
        who={t.t("The platform's cut of the tickets and party adventures orgs sell, taken from the sale; the rest goes to the org's Stripe account. All 0 turns it off.")}
        keys={["fee_percent_bps", "fee_fixed_cents", "fee_min_cents"]}
        saved={saved}
        draft={draft}
        parsed={parsed}
        onSaved={onSaved}
        {...(fee > 0 && platformFeeOn(saved, 19900) === 0 && sellers.length > 0 ? {
          confirm: {
            title: t.plural(sellers.length, { one: t.t("Pause sales for {{n}} org?"), other: t.t("Pause sales for {{n}} orgs?") }),
            message: t.t("With a platform fee, the listings of orgs without a connected Stripe account stop selling until they connect one."),
            confirmLabel: t.t("Save and pause"),
          },
        } : {})}
        example={
          fee > 0
            ? t.t("On a {{price}} ticket the platform keeps {{fee}} and the org gets {{rest}}.", { price: kr(19900), fee: kr(fee), rest: kr(19900 - fee) })
            : t.t("No platform fee: orgs keep their whole sale.")
        }
      >
        <Row>
          <Field label={t.t("Percent of the price")}>{input("fee_percent_bps", "%")}</Field>
          <Field label={t.t("Plus a fixed part")}>{input("fee_fixed_cents", krIncl)}</Field>
          <Field label={t.t("At least")}>{input("fee_min_cents", krIncl)}</Field>
        </Row>
        {sellers.length > 0 && (
          <Callout variant={fee > 0 ? "warning" : "info"}>
            {fee > 0
              ? t.t("These orgs have listings in the store but no Stripe account that can take payments, so their sales are paused until they connect Stripe:")
              : t.t("With a platform fee, these orgs' listings stop selling until they connect a Stripe account:")}
            <ul>
              {sellers.map((o) => (
                <li key={o.id}>{o.name} · {t.plural(o.listings, { one: t.t("{{n}} listing"), other: t.t("{{n}} listings") })}</li>
              ))}
            </ul>
          </Callout>
        )}
      </PriceCard>
      <PayoutTermsCard saved={saved} onSaved={onSaved} />
    </>
  );
}

/** Whether the prices include VAT, which every price shown to orgs then says. */
function VATCard({ saved, onSaved }: { saved: Pricing; onSaved: (p: Pricing) => void }) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const toggle = async (excluded: boolean) => {
    setBusy(true);
    try {
      onSaved(await put<Pricing>("/api/admin/pricing", { ...saved, vat_excluded: excluded }));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t.t("VAT")} subtitle={t.t("Whether the platform's prices include VAT. Every price orgs see says so after it.")}>
      <label className="cs-row cs-row--sm">
        <input type="checkbox" checked={!saved.vat_excluded} disabled={busy} onChange={(e) => void toggle(!e.target.checked)} />
        <span>{t.t("Prices include VAT")}</span>
      </label>
    </Card>
  );
}

/** When orgs are paid for their ticket sales while there is no platform fee, shown on their Billing page. */
function PayoutTermsCard({ saved, onSaved }: { saved: Pricing; onSaved: (p: Pricing) => void }) {
  const t = useT();
  const toast = useToast();
  const [text, setText] = useState(saved.payout_terms);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    try {
      onSaved(await put<Pricing>("/api/admin/pricing", { ...saved, payout_terms: text.trim() }));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card
      title={t.t("Payout terms")}
      subtitle={t.t("With no platform fee, ticket sales are paid to the platform's account. Say when and how orgs get their money; it shows on their Billing page.")}
      actions={
        <Button variant="primary" loading={busy} disabled={text.trim() === saved.payout_terms} onClick={() => void save()}>
          {t.t("Save")}
        </Button>
      }
    >
      <Field label={t.t("Payout terms")} hint={t.t("Empty shows the built-in terms: {{terms}}", { terms: t.t(DEFAULT_PAYOUT_TERMS) })}>
        <textarea rows={3} maxLength={1000} value={text} onChange={(e) => setText(e.target.value)} />
      </Field>
    </Card>
  );
}

/** The payout terms an org sees when the platform admin has set none. */
export const DEFAULT_PAYOUT_TERMS =
  "The platform collects your ticket sales and pays them out to your organisation once a month, for the month before, less refunds. Questions about a payout go to the platform's contact address.";

/** One price's card: its fields, an example in the typed prices, and its own Save. */
function PriceCard(props: {
  title: string;
  who: string;
  keys: Price[];
  saved: Pricing;
  draft: Draft;
  parsed: Pricing | Price;
  example: string;
  onSaved: (p: Pricing) => void;
  /** Asked before saving, when saving has side effects worth a second look. */
  confirm?: { title: string; message: string; confirmLabel: string };
  children: ReactNode;
}) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState(false);
  const bad = typeof props.parsed === "string" && props.keys.includes(props.parsed);
  const mine = typeof props.parsed === "string" ? null : props.parsed;
  const changed = !!mine && props.keys.some((k) => mine[k] !== props.saved[k]);
  const save = async () => {
    if (!mine) return;
    if (props.confirm && !(await dialogs.confirm({ ...props.confirm, danger: true }))) return;
    // Only this card's prices change; the others stay as saved.
    const next = { ...props.saved };
    for (const k of props.keys) next[k] = mine[k];
    setBusy(true);
    try {
      props.onSaved(await put<Pricing>("/api/admin/pricing", next));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card
      title={props.title}
      subtitle={props.who}
      actions={
        <Button variant="primary" loading={busy} disabled={!changed || bad} onClick={() => void save()}>
          {t.t("Save")}
        </Button>
      }
    >
      {props.children}
      <p className="cs-field__hint st-example" role="status">
        {bad ? t.t("Use whole kronor and öre, 0 or more.") : props.example}
      </p>
    </Card>
  );
}
