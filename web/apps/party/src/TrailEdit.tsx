import { useEffect, useMemo, useState } from "react";
import { ApiError } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import { Editor, problems, useTrailSave, type TrailApi } from "@zonventure/trail";
import { useT } from "./i18n.tsx";
import {
  checkout,
  deleteDraft,
  getDraft,
  getHostTrail,
  heldTrails,
  holdTrail,
  saveDraft,
  saveHostTrail,
  saveToEmail,
  shareURL,
  trailSession,
  type PublicTrail,
} from "./trail.ts";

const MINE = "/app/party/trail/mine";

/** What the fragment names: a draft (#id, or #id.secret from a copied link) or, by host link, its trail. */
function target(host: boolean): { id: number } | { token: string } | null {
  const hash = decodeURIComponent(location.hash.replace(/^#/, ""));
  if (host) return hash ? { token: hash } : null;
  const m = /^(\d+)(?:\.([A-Za-z0-9_-]+))?$/.exec(hash);
  if (!m) return null;
  const id = Number(m[1]);
  if (m[2]) {
    // A copied link: this browser holds the draft from now on, and the
    // secret leaves the address bar.
    holdTrail({ id, secret: m[2] });
    history.replaceState(null, "", `#${id}`);
  }
  return { id };
}

/**
 * The public creator: the same editor as the console's, saved to the
 * draft (or, from the host page, to the trail the host link runs). Done
 * goes to checkout while the trail is unpaid.
 */
export function TrailEdit({ host }: { host: boolean }) {
  const t = useT().t;
  const [which] = useState(() => target(host));
  const [doc, setDoc] = useState<PublicTrail | null>(null);
  const [missing, setMissing] = useState(false);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    if (!which) {
      setMissing(true);
      return;
    }
    const load = "token" in which ? getHostTrail(which.token) : getDraft(which.id);
    load.then(setDoc, (e) => {
      if (e instanceof ApiError && e.status === 404) setMissing(true);
      else setMissing(true);
    });
  }, [which, nonce]);

  if (missing || !which) {
    return (
      <section className="party-card">
        <h1>{t("This quiz trail can't be found")}</h1>
        <p>{t("It may have been deleted, or it was made in another browser. Open it from the link in your email, or from My trails.")}</p>
        <a className="party-btn" href={MINE}>
          {t("My trails")}
        </a>
      </section>
    );
  }
  if (!doc) return <p className="party-muted">{t("Loading…")}</p>;
  return <Creator key={doc.revision + ":" + nonce} doc={doc} which={which} reload={() => setNonce((n) => n + 1)} />;
}

function Creator({ doc, which, reload }: { doc: PublicTrail; which: { id: number } | { token: string }; reload: () => void }) {
  const i18n = useT();
  const t = i18n.t;
  const byHost = "token" in which;
  const api = useMemo<TrailApi>(
    () => ({ save: (body) => ("token" in which ? saveHostTrail(which.token, body) : saveDraft(which.id, body)) }),
    [which],
  );
  const s = useTrailSave(api, doc);
  const live = s.doc as PublicTrail;
  const [focus, setFocus] = useState<{ id: string; nonce: number } | null>(null);
  const [tried, setTried] = useState(false);
  const [paying, setPaying] = useState(false);
  const [panel, setPanel] = useState<"email" | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const key = "id" in which ? heldTrails().find((k) => k.id === which.id) : undefined;
  const ended = live.state === "ended";

  const done = async () => {
    setBusy(true);
    try {
      if (!(await s.flush())) return;
      if (byHost) {
        location.href = `/app/party/host/${encodeURIComponent(which.token)}`;
        return;
      }
      if (live.state !== "draft") {
        location.href = MINE;
        return;
      }
      const first = problems(s.cards)[0];
      setTried(true);
      if (first) {
        if (first.card) setFocus({ id: first.card, nonce: Date.now() });
        setNote(first.card ? t("Finish the marked cards first.") : t("Add at least one question first."));
        return;
      }
      setPaying(true);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!("id" in which) || !confirm(t("Delete this quiz trail? This can't be undone."))) return;
    try {
      await deleteDraft(which.id);
      location.href = MINE;
    } catch (e) {
      setNote(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
    }
  };

  const copy = async () => {
    if (!key) return;
    try {
      await navigator.clipboard.writeText(shareURL(key));
      setNote(t("Link copied. Anyone with it can edit this quiz trail."));
    } catch {
      prompt(t("Copy this link:"), shareURL(key));
    }
  };

  if (paying && "id" in which) return <Checkout doc={live} id={which.id} cards={s.cards.length} back={() => setPaying(false)} />;

  const saved = live.email ? t("Saved to {{email}}", { email: live.email }) : byHost ? undefined : t("Saved on this phone");
  return (
    <div className="party-trail">
      {live.round_open && <p className="party-note">{t("A game is being played. Changes apply from the next game.")}</p>}
      {live.reprint && live.state !== "draft" && (
        <p className="party-note">{t("The cards changed since you printed them. Print them again from the admin page before the next game.")}</p>
      )}
      {ended && <p className="party-note">{t("This quiz trail has ended.")}</p>}
      {note && (
        <p className="party-note" role="status">
          {note}
        </p>
      )}
      {panel === "email" && <SaveToEmail close={() => setPanel(null)} />}
      <Editor
        i18n={i18n}
        title={s.title}
        cards={s.cards}
        state={s.state}
        readOnly={s.readOnly || ended}
        onChange={s.change}
        focus={focus}
        showProblems={tried}
        savedText={saved}
        actions={
          <>
            {s.state === "conflict" && (
              <button type="button" className="trail-btn" onClick={reload}>
                {t("Reload")}
              </button>
            )}
            {!byHost && (
              <details className="party-menu">
                <summary className="trail-btn">{t("Menu")}</summary>
                <div className="party-menu__items">
                  {!live.email && (
                    <button type="button" onClick={() => setPanel("email")}>
                      {t("Save to email")}
                    </button>
                  )}
                  {key && (
                    <button type="button" onClick={() => void copy()}>
                      {t("Copy link to this quiz trail")}
                    </button>
                  )}
                  <a href={MINE}>{t("My trails")}</a>
                  {live.state === "draft" && (
                    <button type="button" className="is-danger" onClick={() => void remove()}>
                      {t("Delete")}
                    </button>
                  )}
                </div>
              </details>
            )}
            {!ended && (
              <button type="button" className="trail-btn trail-btn--primary" disabled={busy} onClick={() => void done()}>
                {t("Done")}
              </button>
            )}
          </>
        }
      />
    </div>
  );
}

/** Save to email: a link confirms the address and saves every trail this browser holds there. */
function SaveToEmail({ close }: { close: () => void }) {
  const t = useT().t;
  const [email, setEmail] = useState(trailSession()?.email ?? "");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <section className="party-card">
      <h2>{t("Save to email")}</h2>
      {sent ? (
        <>
          <p>{t("We've sent you a link. Open it to save your quiz trails to {{email}}.", { email })}</p>
          <button type="button" className="party-btn" onClick={close}>
            {t("Close")}
          </button>
        </>
      ) : (
        <form
          className="party-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(null);
            try {
              await saveToEmail(email.trim());
              setSent(true);
            } catch (err) {
              setError(err instanceof ApiError ? err.message : t("Something went wrong. Try again."));
            } finally {
              setBusy(false);
            }
          }}
        >
          <p className="party-muted">{t("Then you can open your quiz trails from any phone or computer. No password.")}</p>
          <label className="party-field">
            <span>{t("Your email")}</span>
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
          </label>
          {error && (
            <p className="party-error" role="alert">
              {error}
            </p>
          )}
          <div className="party-actions">
            <button className="party-btn party-btn--primary" disabled={busy}>
              {t("Send the link")}
            </button>
            <button type="button" className="party-btn" onClick={close}>
              {t("Cancel")}
            </button>
          </div>
        </form>
      )}
    </section>
  );
}

/** Checkout: what the pass buys, how many teams, the email for the admin link, then pay. */
function Checkout({ doc, id, cards, back }: { doc: PublicTrail; id: number; cards: number; back: () => void }) {
  const i18n = useT();
  const t = i18n.t;
  const loc = i18n.current();
  const [blocks, setBlocks] = useState(1);
  const [more, setMore] = useState(false);
  const [email, setEmail] = useState(trailSession()?.email ?? "");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const per = doc.price.teams_per_block;
  const free = doc.price.block_cents <= 0;
  const total = doc.price.block_cents * blocks;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await checkout(id, { email: email.trim(), blocks, consent });
      if (r.checkout_url) location.href = r.checkout_url;
      else if (r.host_url) location.href = r.host_url;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("Something went wrong. Try again."));
      setBusy(false);
    }
  };

  return (
    <section className="party-card">
      <p className="party-eyebrow">{t("Quiz trail")}</p>
      <h1>{doc.title}</h1>
      <ul className="party-points">
        <li>{t("{{n}} cards", { n: cards })}</li>
        <li>{t("Play as many games as you like for 14 days from the first game. Start within 60 days.")}</li>
        <li>{t("Up to {{n}} teams in one game, each on a phone. No app and no accounts.", { n: blocks * per })}</li>
      </ul>
      {!doc.payments_open ? (
        <>
          <p className="party-note">{t("Booking isn't open right now. Your quiz trail is saved; check back soon.")}</p>
          <button type="button" className="party-btn" onClick={back}>
            {t("Back to the cards")}
          </button>
        </>
      ) : (
        <form className="party-form" onSubmit={submit}>
          {!more ? (
            <button type="button" className="party-link" onClick={() => setMore(true)}>
              {t("More than {{n}} teams in one game?", { n: per })}
            </button>
          ) : (
            <label className="party-field">
              <span>{t("Teams in one game")}</span>
              <div className="party-stepper">
                <button type="button" onClick={() => setBlocks((b) => Math.max(1, b - 1))} disabled={blocks <= 1} aria-label={t("Fewer")}>
                  −
                </button>
                <output aria-live="polite">{t("up to {{n}}", { n: blocks * per })}</output>
                <button type="button" onClick={() => setBlocks((b) => Math.min(10, b + 1))} disabled={blocks >= 10} aria-label={t("More")}>
                  +
                </button>
              </div>
            </label>
          )}
          <label className="party-field">
            <span>{t("Your email")}</span>
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
            <small className="party-muted">{t("We send the link you run the quiz trail from here.")}</small>
          </label>
          {doc.kids && (
            <label className="party-check">
              <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} required />
              <span>{t("I am the guardian of the children who will play, or I have their guardians' consent.")}</span>
            </label>
          )}
          {!free && (
            <div className="party-total">
              <span>{blocks > 1 ? t("{{price}} per {{n}} teams", { price: money(doc.price.block_cents, doc.price.currency, loc), n: per }) : t("Total")}</span>
              <strong>{money(total, doc.price.currency, loc)}</strong>
            </div>
          )}
          {error && (
            <p className="party-error" role="alert">
              {error}
            </p>
          )}
          <div className="party-actions">
            <button className="party-btn party-btn--primary" disabled={busy || !email || (doc.kids && !consent)}>
              {busy ? t("Opening checkout…") : free ? t("Get it ready to play") : t("Pay {{total}}", { total: money(total, doc.price.currency, loc) })}
            </button>
            <button type="button" className="party-btn" onClick={back}>
              {t("Back to the cards")}
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
