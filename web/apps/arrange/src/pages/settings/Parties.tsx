import { useState } from "react";
import { ExternalLink } from "lucide-react";
import { ApiError, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Row, useLoad, useToast } from "../../ui/index.ts";

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

/**
 * Parties: which of the org's adventures a parent can book for a
 * children's party, and at what price. A party plays the adventure's
 * places as cards the host prints and hides, so an adventure that asks
 * where each player stands cannot be offered.
 */
export function PartiesTab() {
  const t = useT();
  const load = useLoad((signal) => request<{ offers: PartyOffer[] }>("/api/admin/party-offers", { signal }), []);
  const about = (
    <HelpPopover
      label={t.t("About parties")}
      body={t.t(
        "A parent books a party on your page and pays, with no account. They get a host page to set up teams, print cards and start the game; everything about the players is deleted a while after the party.",
      )}
    />
  );
  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const offers = load.data.offers;
  return (
    <Card title={t.t("Parties")} subtitle={t.t("Let parents book your adventures for children's parties.")} actions={about}>
      {offers.length === 0 ? (
        <Empty title={t.t("No adventures yet")}>{t.t("Create an adventure, or have one shared with you, to offer it as a party.")}</Empty>
      ) : (
        <div className="pt-offers">
          {offers.map((o) => (
            <OfferForm key={o.adventure_slug} offer={o} onSaved={load.reload} />
          ))}
        </div>
      )}
    </Card>
  );
}

const major = (cents: number) => (cents / 100).toString();
const minor = (s: string) => Math.round(Number(s.replace(",", ".")) * 100) || 0;

function OfferForm({ offer, onSaved }: { offer: PartyOffer; onSaved: () => void }) {
  const t = useT();
  const toast = useToast();
  const [enabled, setEnabled] = useState(offer.enabled);
  const [base, setBase] = useState(major(offer.base_cents));
  const [perKid, setPerKid] = useState(major(offer.per_kid_cents));
  const [currency, setCurrency] = useState(offer.currency);
  const [maxKids, setMaxKids] = useState(String(offer.max_kids));
  const [blurb, setBlurb] = useState(offer.blurb);
  const [saving, setSaving] = useState(false);
  const blocked = (offer.problems?.length ?? 0) > 0;

  const save = async () => {
    setSaving(true);
    try {
      await request(`/api/admin/party-offers/${encodeURIComponent(offer.adventure_slug)}`, {
        method: "PUT",
        body: { enabled, base_cents: minor(base), per_kid_cents: minor(perKid), currency, max_kids: Number(maxKids), blurb },
      });
      toast.ok(t.t("Saved"));
      onSaved();
    } catch (e) {
      toast.err(e instanceof ApiError ? e.message : e);
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="pt-offer">
      <Row>
        <strong>{offer.title}</strong>
        {offer.enabled ? <Pill tone="good">{t.t("Bookable")}</Pill> : <Pill>{t.t("Not offered")}</Pill>}
        {offer.stations > 0 && <span className="pt-muted">{t.t("{{n}} cards to hide", { n: offer.stations })}</span>}
      </Row>
      {blocked && <Callout variant="warning">{t.t("Can't be offered as a party: {{reason}}", { reason: offer.problems![0]! })}</Callout>}
      <label className="pt-switch">
        <input type="checkbox" role="switch" checked={enabled} disabled={blocked && !enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {t.t("Offer as a party")}
      </label>
      <div className="pt-grid">
        <Field label={t.t("Base price")}>
          <input inputMode="decimal" value={base} onChange={(e) => setBase(e.target.value)} />
        </Field>
        <Field label={t.t("Per child")}>
          <input inputMode="decimal" value={perKid} onChange={(e) => setPerKid(e.target.value)} />
        </Field>
        <Field label={t.t("Currency")}>
          <input maxLength={3} value={currency} onChange={(e) => setCurrency(e.target.value.toLowerCase())} />
        </Field>
        <Field label={t.t("Most children")}>
          <input type="number" min={1} max={50} value={maxKids} onChange={(e) => setMaxKids(e.target.value)} />
        </Field>
      </div>
      <Field label={t.t("Description on the booking page")}>
        <textarea rows={2} value={blurb} onChange={(e) => setBlurb(e.target.value)} />
      </Field>
      <Row>
        <Button variant="primary" loading={saving} onClick={() => void save()}>
          {t.t("Save")}
        </Button>
        {offer.enabled && offer.book_url && (
          <a className="pt-link" href={offer.book_url} target="_blank" rel="noopener noreferrer">
            <ExternalLink size={ICON.sm} aria-hidden /> {t.t("Open the booking page")}
          </a>
        )}
      </Row>
    </section>
  );
}
