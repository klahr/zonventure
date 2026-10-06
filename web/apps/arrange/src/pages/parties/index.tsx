import { useState } from "react";
import { Copy, ExternalLink, Plus } from "lucide-react";
import { ApiError, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Callout, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Page, PageHeader, Pill, Table, fmtMoney, useLoad, useToast, type Column } from "../../ui/index.ts";
import { listAdventures, listEvents } from "../events/api.ts";
import { AdventureCover, EventsTable } from "../events/parts.tsx";
import "../events/events.css";
import "./parties.css";

/** internal/admin/party.go partyOfferDTO */
interface PartyOffer {
  adventure_slug: string;
  title: string;
  offer_id?: number;
  enabled: boolean;
  base_cents: number;
  per_kid_cents: number;
  currency: string;
  max_kids: number;
  blurb: string;
  stations: number;
  problems?: string[];
  book_url?: string;
}

/** An offer with its adventure's cover, which the offers list leaves out. */
type Row = PartyOffer & { cover?: string | undefined };

const CURRENCIES = ["sek", "eur", "nok", "dkk", "gbp", "usd"];

/**
 * Hosted: the org's hosted events — private events their hosts run
 * from a host page, game after game — and, for org admins, the offers
 * they are booked from.
 */
export function PartiesPage({ slug }: { slug?: string | undefined }) {
  const t = useT();
  const { state } = useSession();
  const admin = state.status === "authenticated" && state.user.role === "org_admin";
  const hosted = useLoad((signal) => listEvents(undefined, signal, true), []);
  return (
    <Page>
      <PageHeader title={t.t("Hosted")} subtitle={t.t("Private events their hosts run themselves, game after game, with no account.")} />
      <Card title={t.t("Hosted events")} flush>
        {hosted.error && !hosted.data ? (
          <ErrorBox message={hosted.error} retry={hosted.reload} retryLabel={t.t("Retry")} />
        ) : !hosted.data ? (
          <Loading label={t.t("Loading…")} />
        ) : (
          <EventsTable rows={hosted.data} empty={t.t("No hosted events yet. They appear here when someone books one of your offers.")} />
        )}
      </Card>
      {admin && <Offers slug={slug} />}
    </Page>
  );
}

/**
 * The org's adventures a parent can book as a hosted event, and at what
 * price. The list holds the offers made; a new one picks from the
 * adventures a hosted event can play — as cards the host prints and
 * hides, or in the host's park — and the rest are only named, with the
 * reason, at the foot. "#/parties/<slug>" opens an offer to edit.
 */
function Offers({ slug }: { slug?: string | undefined }) {
  const t = useT();
  const loc = t.current();
  const toast = useToast();
  const load = useLoad(async (signal) => {
    const [offers, advs] = await Promise.all([
      request<{ offers: PartyOffer[] }>("/api/admin/party-offers", { signal }).then((r) => r.offers),
      listAdventures(signal).catch(() => []),
    ]);
    const covers = new Map(advs.map((a) => [a.slug, a.avatar_url]));
    return offers.map((o): Row => ({ ...o, cover: covers.get(o.adventure_slug) }));
  }, []);
  const [creating, setCreating] = useState(false);

  const rows = load.data ?? [];
  const offered = rows.filter((o) => o.offer_id);
  const available = rows.filter((o) => !o.offer_id && !o.problems?.length);
  const unavailable = rows.filter((o) => !o.offer_id && o.problems?.length);
  const editing = slug ? offered.find((o) => o.adventure_slug === slug) : undefined;
  const closeEdit = () => navigate({ name: "parties" });

  const copyLink = async (o: Row) => {
    try {
      await navigator.clipboard.writeText(new URL(o.book_url!, location.origin).toString());
      toast.ok(t.t("Booking link copied"));
    } catch (e) {
      toast.err(e);
    }
  };

  const newBtn = (
    <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setCreating(true)} disabled={available.length === 0}>
      {t.t("New party offer")}
    </Button>
  );
  const about = (
    <HelpPopover
      label={t.t("About parties")}
      body={t.t(
        "A parent books a party on your page and pays, with no account. They get a host page to print the cards and share the player link; teams join and play game after game for 14 days, then everything about the players is deleted.",
      )}
    />
  );

  const columns: Column<Row>[] = [
    { key: "name", header: t.t("Party"), cell: (o) => (
      <span className="evs-namecell">
        <AdventureCover title={o.title} src={o.cover} />
        <span className="evs-stack">
          <strong>{o.title}</strong>
          {o.stations > 0 && <span className="evs-sub">{t.t("{{n}} cards to hide", { n: o.stations })}</span>}
        </span>
      </span>
    ) },
    { key: "price", header: t.t("Price"), align: "end", cell: (o) => <PriceCell offer={o} locale={loc} /> },
    { key: "kids", header: t.t("Most children"), wide: true, align: "end", cell: (o) => String(o.max_kids) },
    { key: "state", header: t.t("State"), align: "end", cell: (o) => <StatePill offer={o} /> },
    { key: "link", header: <span className="cs-sr">{t.t("Booking page")}</span>, align: "end", width: "1%", cell: (o) =>
      o.enabled && o.book_url ? (
        <span className="pt-actions">
          <Button size="icon" variant="ghost" onClick={() => void copyLink(o)} aria-label={t.t("Copy booking link") + " — " + o.title} title={t.t("Copy booking link")}>
            <Copy size={ICON.sm} aria-hidden />
          </Button>
          <a className="btn ghost icon" href={o.book_url} target="_blank" rel="noopener noreferrer" aria-label={t.t("Open the booking page") + " — " + o.title} title={t.t("Open the booking page")}>
            <ExternalLink size={ICON.sm} aria-hidden />
          </a>
        </span>
      ) : null },
  ];

  let body;
  if (load.error && !load.data) body = <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  else if (!load.data) body = <Loading label={t.t("Loading…")} />;
  else if (offered.length === 0)
    body = available.length > 0 ? (
      <Empty title={t.t("No party offers yet.")} action={newBtn}>
        {t.t("Pick an adventure, set a price, and share the booking page with parents.")}
      </Empty>
    ) : (
      <Empty title={t.t("None of your adventures can be a party yet.")}>
        {t.t("A party plays without asking where each player stands. See below what stops each adventure.")}
      </Empty>
    );
  else body = <Table columns={columns} rows={offered} rowKey={(o) => o.adventure_slug} rowHref={(o) => href({ name: "parties", slug: o.adventure_slug })} caption={t.t("Party offers")} />;

  return (
    <>
      <Card
        title={t.t("Offers")}
        subtitle={t.t("Children's parties parents book and pay for on your booking page.")}
        actions={<>{about}{offered.length > 0 && newBtn}</>}
        flush={offered.length > 0}
      >
        {body}
      </Card>
      {unavailable.length > 0 && (
        <details className="pt-unavailable">
          <summary>{t.t("{{n}} adventures can't be offered as a party", { n: unavailable.length })}</summary>
          <ul>
            {unavailable.map((o) => (
              <li key={o.adventure_slug}>
                <strong>{o.title}</strong>
                <span className="evs-muted">{sentence(o.problems![0]!)}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
      {creating && <OfferDialog choices={available} onClose={() => setCreating(false)} onSaved={load.reload} />}
      {editing && <OfferDialog key={editing.adventure_slug} offer={editing} onClose={closeEdit} onSaved={load.reload} />}
    </>
  );
}

function PriceCell({ offer, locale }: { offer: Row; locale: string }) {
  const t = useT();
  const base = fmtMoney(offer.base_cents, offer.currency, locale);
  if (!offer.per_kid_cents) return <>{base}</>;
  const perKid = fmtMoney(offer.per_kid_cents, offer.currency, locale);
  return (
    <span className="evs-stack pt-price">
      <span>{base}</span>
      <span className="evs-sub">{t.t("+ {{price}} per child", { price: perKid })}</span>
    </span>
  );
}

function StatePill({ offer }: { offer: Row }) {
  const t = useT();
  const problem = offer.problems?.[0];
  if (problem) return <Pill tone="warn" title={sentence(problem)}>{t.t("Can't be booked")}</Pill>;
  return offer.enabled ? <Pill tone="good">{t.t("Bookable")}</Pill> : <Pill>{t.t("Paused")}</Pill>;
}

/** The server's reasons are lowercase clauses; shown alone, they read as sentences. */
const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

const major = (cents: number) => (cents ? (cents / 100).toString() : "");
const minor = (s: string) => Math.round(Number(s.replace(",", ".")) * 100) || 0;

/**
 * One offer's terms. Given an offer it edits that one; given choices it
 * makes a new offer for one of them.
 */
function OfferDialog(props: { offer?: Row; choices?: Row[]; onClose: () => void; onSaved: () => void }) {
  const t = useT();
  const loc = t.current();
  const toast = useToast();
  const isNew = !props.offer;
  const [picked, setPicked] = useState(props.offer?.adventure_slug ?? props.choices?.[0]?.adventure_slug ?? "");
  const target = props.offer ?? props.choices?.find((c) => c.adventure_slug === picked);
  const start = props.offer ?? props.choices?.[0];
  const [enabled, setEnabled] = useState(props.offer ? props.offer.enabled : true);
  const [base, setBase] = useState(major(start?.base_cents ?? 0));
  const [perKid, setPerKid] = useState(major(start?.per_kid_cents ?? 0));
  const [currency, setCurrency] = useState(start?.currency || "sek");
  const [maxKids, setMaxKids] = useState(String(start?.max_kids ?? 20));
  const [blurb, setBlurb] = useState(start?.blurb ?? "");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const blocked = (target?.problems?.length ?? 0) > 0;
  const currencies = CURRENCIES.includes(currency) ? CURRENCIES : [currency, ...CURRENCIES];

  const kidsN = Number(maxKids);
  const example = Math.min(10, kidsN || 10);
  const exampleCents = minor(base) + minor(perKid) * example;

  async function submit() {
    if (busy || !target) return;
    setError(null);
    if (!Number.isInteger(kidsN) || kidsN < 1 || kidsN > 50) return setError(t.t("Most children must be between 1 and 50"));
    if (enabled && exampleCents <= 0) return setError(t.t("Set a price before the party can be booked"));
    setBusy(true);
    try {
      await request(`/api/admin/party-offers/${encodeURIComponent(target.adventure_slug)}`, {
        method: "PUT",
        body: { enabled: enabled && !blocked, base_cents: minor(base), per_kid_cents: minor(perKid), currency, max_kids: kidsN, blurb },
      });
      toast.ok(isNew ? t.t("Party offer created") : t.t("Saved"));
      props.onSaved();
      props.onClose();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setBusy(false);
    }
  }

  return (
    <Dialog
      title={isNew ? t.t("New party offer") : t.t("Party offer")}
      wide
      onClose={() => !busy && props.onClose()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={props.onClose} disabled={busy}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{isNew ? t.t("Create offer") : t.t("Save")}</Button>
        </>
      }
    >
      {isNew && (props.choices?.length ?? 0) > 1 ? (
        <fieldset className="evs-fieldset">
          <legend className="cs-field__label">{t.t("Adventure")}</legend>
          <div className="evs-picker" role="radiogroup" aria-label={t.t("Adventure")}>
            {props.choices!.map((a) => (
              <label key={a.adventure_slug} className={"evs-choice" + (a.adventure_slug === picked ? " is-on" : "")}>
                <input type="radio" name="pt-adventure" checked={a.adventure_slug === picked} onChange={() => setPicked(a.adventure_slug)} />
                <AdventureCover title={a.title} src={a.cover} />
                <span className="evs-stack">
                  <strong className="evs-ellipsis">{a.title}</strong>
                  <span className="evs-muted evs-small">{t.t("{{n}} cards to hide", { n: a.stations })}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
      ) : (
        target && (
          <div className="evs-chosen">
            <AdventureCover title={target.title} src={target.cover} size={36} />
            <span className="evs-stack">
              <strong>{target.title}</strong>
              {target.stations > 0 && <span className="evs-muted evs-small">{t.t("{{n}} cards to hide", { n: target.stations })}</span>}
            </span>
          </div>
        )
      )}
      {blocked && <Callout variant="warning">{t.t("Can't be offered as a party: {{reason}}", { reason: target!.problems![0]! })}</Callout>}
      <div className="pt-terms">
        <Field label={t.t("Base price")}>
          <input inputMode="decimal" value={base} onChange={(e) => setBase(e.target.value)} placeholder="0" />
        </Field>
        <Field label={t.t("Per child")}>
          <input inputMode="decimal" value={perKid} onChange={(e) => setPerKid(e.target.value)} placeholder="0" />
        </Field>
        <Field label={t.t("Currency")}>
          <select value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {currencies.map((c) => <option key={c} value={c}>{c.toUpperCase()}</option>)}
          </select>
        </Field>
        <Field label={t.t("Most children")}>
          <input type="number" min={1} max={50} value={maxKids} onChange={(e) => setMaxKids(e.target.value)} />
        </Field>
      </div>
      {exampleCents > 0 && (
        <p className="evs-muted evs-small">
          {t.t("A party of {{n}} children costs {{price}}.", { n: example, price: fmtMoney(exampleCents, currency, loc) })}
        </p>
      )}
      <Field label={t.t("Description on the booking page")}>
        <textarea rows={3} maxLength={1000} value={blurb} onChange={(e) => setBlurb(e.target.value)} placeholder={t.t("What the children will do, how long it takes, what to bring.")} />
      </Field>
      <label className="evs-check">
        <input type="checkbox" checked={enabled && !blocked} disabled={blocked} onChange={(e) => setEnabled(e.target.checked)} />
        <span>{t.t("Bookable: parents can book it on your booking page")}</span>
      </label>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}
