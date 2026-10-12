import { useEffect, useState, type ReactNode } from "react";
import { get, put } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Field, Loading, Row, fmtMoney, useLoad, useToast } from "../../ui/index.ts";

/** pricingResponse, internal/admin/pricing.go. Amounts in öre, VAT included. */
interface Pricing {
  player_cents: number;
  free_players: number;
  hosted_block_cents: number;
  fee_percent_bps: number;
  fee_fixed_cents: number;
  fee_min_cents: number;
}

/** What the fields hold while typed: kronor and percent as written. */
type Draft = Record<keyof Pricing, string>;

const KRONOR: (keyof Pricing)[] = ["player_cents", "hosted_block_cents", "fee_fixed_cents", "fee_min_cents"];

function toDraft(p: Pricing): Draft {
  const d = {} as Draft;
  for (const k of Object.keys(p) as (keyof Pricing)[]) {
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
function fromDraft(d: Draft): Pricing | keyof Pricing {
  const p = {} as Pricing;
  for (const k of Object.keys(d) as (keyof Pricing)[]) {
    const n = num(d[k]);
    const v = KRONOR.includes(k) || k === "fee_percent_bps" ? Math.round(n * 100) : n;
    if (!Number.isFinite(n) || n < 0 || !Number.isInteger(v)) return k;
    p[k] = v;
  }
  return p;
}

/** payments.PlatformFee.Compute: the cut of a sale, kept below the price. */
function feeOn(p: Pricing, price: number): number {
  if (!(p.fee_percent_bps || p.fee_fixed_cents || p.fee_min_cents) || price <= 0) return 0;
  const fee = Math.max(Math.floor((price * p.fee_percent_bps) / 10000) + p.fee_fixed_cents, p.fee_min_cents);
  return Math.max(0, Math.min(fee, price - 1));
}

/**
 * What the platform charges, for the platform admin: the event pass,
 * the hosted pass and the cut of what orgs sell. In SEK, VAT included.
 * Each card saves on its own and applies to the next checkout; nothing
 * is set in Stripe.
 */
export function PricingTab() {
  const t = useT();
  const load = useLoad((signal) => get<Pricing>("/api/admin/pricing", { signal }), []);
  const [draft, setDraft] = useState<Draft | null>(null);
  useEffect(() => {
    if (load.data) setDraft(toDraft(load.data));
  }, [load.data]);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data || !draft) return <Loading label={t.t("Loading…")} />;
  const saved = load.data;
  const parsed = fromDraft(draft);
  const now = typeof parsed === "string" ? saved : parsed;
  const kr = (cents: number) => fmtMoney(cents, "SEK", t.current());
  const set = (k: keyof Pricing) => (e: { target: { value: string } }) => setDraft({ ...draft, [k]: e.target.value });
  const input = (k: keyof Pricing, suffix: string) => (
    <span className="cs-row cs-row--sm">
      <input className="st-num" inputMode="decimal" value={draft[k]} onChange={set(k)} aria-invalid={parsed === k} />
      <span>{suffix}</span>
    </span>
  );
  const krIncl = t.t("kr incl. VAT");
  const fee = feeOn(now, 19900);

  return (
    <>
      <p className="cs-field__hint st-intro">
        {t.t("What the platform charges, in SEK including VAT. A saved price applies to the next checkout; purchases already made or started keep theirs.")}
      </p>
      <PriceCard
        title={t.t("Event pass")}
        who={t.t("Orgs pay it for a scheduled event, to the platform's account.")}
        keys={["player_cents", "free_players"]}
        saved={saved}
        draft={draft}
        parsed={parsed}
        onSaved={(p) => load.set(p)}
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
        onSaved={(p) => load.set(p)}
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
        onSaved={(p) => load.set(p)}
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
      </PriceCard>
    </>
  );
}

/** One price's card: its fields, an example in the typed prices, and its own Save. */
function PriceCard(props: {
  title: string;
  who: string;
  keys: (keyof Pricing)[];
  saved: Pricing;
  draft: Draft;
  parsed: Pricing | keyof Pricing;
  example: string;
  onSaved: (p: Pricing) => void;
  children: ReactNode;
}) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const bad = typeof props.parsed === "string" && props.keys.includes(props.parsed);
  const mine = typeof props.parsed === "string" ? null : props.parsed;
  const changed = !!mine && props.keys.some((k) => mine[k] !== props.saved[k]);
  const save = async () => {
    if (!mine) return;
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
