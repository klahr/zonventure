import { useEffect, useState } from "react";
import { ApiError } from "@zonventure/api";
import { coverOf } from "@zonventure/play-ui";
import { SPOTS } from "@zonventure/ui";
import { book, getOffer, money, resend, type Offer } from "./api.ts";
import { useT } from "./i18n.tsx";
import { useLook } from "./look.ts";
import { Spot, ThankYouBanner } from "./pictures.tsx";

/**
 * Booking a party: how many children, an email for the host link, and the
 * guardian declaration, then Stripe's checkout. Back from checkout, the
 * page says to look for the email.
 */
export function Book({ org, offer: offerID }: { org: string; offer: string }) {
  const i18n = useT();
  const t = i18n.t;
  const params = new URLSearchParams(location.search);
  const paid = params.has("paid");
  const cancelled = params.has("cancelled");

  const [offer, setOffer] = useState<Offer | null>(null);
  const [missing, setMissing] = useState(false);
  const [kids, setKids] = useState(8);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useLook(offer?.look);

  useEffect(() => {
    getOffer(org, offerID).then(
      (o) => {
        setOffer(o);
        setKids((k) => Math.min(k, o.max_kids));
      },
      () => setMissing(true),
    );
  }, [org, offerID]);

  if (paid) return <Paid />;
  if (missing) {
    return (
      <section className="party-card">
        <Spot src={SPOTS.notFound} />
        <h1>{t("This party can't be found")}</h1>
        <p>{t("It may no longer be offered.")}</p>
      </section>
    );
  }
  if (!offer) return <p className="party-muted">{t("Loading…")}</p>;

  const loc = i18n.current();
  const total = offer.base_cents + offer.per_kid_cents * kids;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await book(org, offerID, { email, kids, party_name: name, consent });
      location.href = r.checkout_url;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("Something went wrong. Try again."));
      setBusy(false);
    }
  };

  const cover = coverOf(offer.theme, offer.cover_url);
  return (
    <section className={"party-card" + (cover ? " party-card--cover" : "")}>
      {cover && <div className="party-cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
      <p className="party-eyebrow">{t("A party by {{org}}", { org: offer.org_name })}</p>
      <h1>{offer.title}</h1>
      {offer.blurb && <p className="party-lead">{offer.blurb}</p>}
      <ul className="party-points">
        <li>{t("Played at home, in the garden or in a park: you print the cards and hide them.")}</li>
        <li>{t("Each team plays on a phone. No app and no accounts.")}</li>
        <li>{t("Start whenever you like within 60 days of booking.")}</li>
      </ul>
      {cancelled && <p className="party-note">{t("The payment was cancelled. Nothing was charged.")}</p>}
      {!offer.payments_open ? (
        <p className="party-note">{t("Booking isn't open right now. Check back soon.")}</p>
      ) : (
        <form className="party-form" onSubmit={submit}>
          <label className="party-field">
            <span>{t("Children")}</span>
            <div className="party-stepper">
              <button type="button" onClick={() => setKids((k) => Math.max(1, k - 1))} disabled={kids <= 1} aria-label={t("Fewer")}>
                −
              </button>
              <output aria-live="polite">{kids}</output>
              <button type="button" onClick={() => setKids((k) => Math.min(offer.max_kids, k + 1))} disabled={kids >= offer.max_kids} aria-label={t("More")}>
                +
              </button>
            </div>
            <small className="party-muted">{t("Up to {{n}} children.", { n: offer.max_kids })}</small>
          </label>
          <label className="party-field">
            <span>{t("Name of the party (optional)")}</span>
            <input value={name} maxLength={80} onChange={(e) => setName(e.target.value)} placeholder={t("Elsa turns 8")} />
          </label>
          <label className="party-field">
            <span>{t("Your email")}</span>
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
            <small className="party-muted">{t("We send the link you run the party from here.")}</small>
          </label>
          <label className="party-check">
            <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} required />
            <span>{t("I am the guardian of the children who will play, or I have their guardians' consent.")}</span>
          </label>
          <div className="party-total">
            <span>
              {offer.base_cents > 0 && offer.per_kid_cents > 0
                ? t("{{base}} + {{per}} per child", { base: money(offer.base_cents, offer.currency, loc), per: money(offer.per_kid_cents, offer.currency, loc) })
                : t("Total")}
            </span>
            <strong>{money(total, offer.currency, loc)}</strong>
          </div>
          {error && (
            <p className="party-error" role="alert">
              {error}
            </p>
          )}
          <button className="party-btn party-btn--primary" disabled={busy || !consent || !email}>
            {busy ? t("Opening checkout…") : t("Book and pay {{total}}", { total: money(total, offer.currency, loc) })}
          </button>
        </form>
      )}
    </section>
  );
}

/** Back from checkout: the host link is on its way, or can be sent again. */
function Paid() {
  const t = useT().t;
  return (
    <section className="party-card party-card--cover">
      <ThankYouBanner />
      <h1>{t("Thank you — your party is booked")}</h1>
      <p className="party-lead">{t("We're emailing you the link to your host page. From there you set up the teams, print the cards and start the game.")}</p>
      <p className="party-muted">{t("It can take a minute or two. Check your spam folder if it doesn't arrive.")}</p>
      <Resend />
    </section>
  );
}

/** Sends a booker's host links again, to the booking email. */
export function Resend() {
  const t = useT().t;
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  if (sent) return <p className="party-note">{t("If that email booked a party, a new link is on its way. Older links stop working.")}</p>;
  return (
    <form
      className="party-inline"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        try {
          await resend(email);
        } finally {
          setBusy(false);
          setSent(true);
        }
      }}
    >
      <label className="party-field">
        <span>{t("No email? Send the link again")}</span>
        <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder={t("Your email")} autoComplete="email" />
      </label>
      <button className="party-btn" disabled={busy}>
        {t("Send")}
      </button>
    </form>
  );
}
