import { useEffect, useState } from "react";
import { ApiError } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import { TemplatePicker } from "@zonventure/trail";
import { useT } from "./i18n.tsx";
import {
  createDraft,
  deleteDraft,
  duplicateDraft,
  editURL,
  forgetTrails,
  getStart,
  heldTrails,
  holdTrail,
  listMine,
  sendHostLink,
  setTrailSession,
  signIn,
  trailSession,
  verify,
  type TrailStart as Start,
  type TrailSummary,
} from "./trail.ts";

const MINE = "/app/party/trail/mine";

function MyTrailsLink() {
  const t = useT().t;
  if (heldTrails().length === 0 && !trailSession()) return null;
  return (
    <p className="party-right">
      <a href={MINE}>{t("My trails")}</a>
    </p>
  );
}

/** The start page: what a quiz trail is, what it costs, and a template to begin from. */
export function TrailStart({ org }: { org: string }) {
  const i18n = useT();
  const t = i18n.t;
  const lang = i18n.current().startsWith("sv") ? "sv" : "en";
  const [start, setStart] = useState<Start | null>(null);
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getStart(org, lang).then(setStart, () => setMissing(true));
  }, [org, lang]);

  if (missing || (start && !start.enabled)) {
    return (
      <section className="party-card">
        <h1>{t("Not available")}</h1>
        <p>{t("Quiz trails can't be made here.")}</p>
      </section>
    );
  }
  if (!start) return <p className="party-muted">{t("Loading…")}</p>;

  const pick = async (template: string) => {
    setBusy(true);
    setError(null);
    try {
      const d = await createDraft(org, template, lang);
      holdTrail({ id: d.id, secret: d.secret });
      location.href = editURL(d.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
      setBusy(false);
    }
  };
  const price = start.price.block_cents > 0 ? money(start.price.block_cents, start.price.currency, i18n.current()) : null;

  return (
    <>
      <MyTrailsLink />
      <section className="party-card">
        <h1>{t("Make your own quiz trail")}</h1>
        <p className="party-lead">{t("Write the questions on your phone, print the cards and hide them along the way. The teams answer on their phones and see who won at the end.")}</p>
        <p className="party-price">
          {price
            ? t("Free to make · {{price}} to play for {{days}} days", { price, days: start.play_days })
            : t("Free to make and to play for {{days}} days", { days: start.play_days })}
        </p>
      </section>
      <section className="party-card">
        <h2>{t("Start from")}</h2>
        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}
        <TemplatePicker i18n={i18n} templates={start.templates} busy={busy} onPick={(n) => void pick(n)} />
      </section>
    </>
  );
}

/** My trails: this browser's, and every one saved to the signed-in email. */
export function TrailMine() {
  const i18n = useT();
  const t = i18n.t;
  const [list, setList] = useState<TrailSummary[] | null>(null);
  const [email, setEmail] = useState(trailSession()?.email ?? "");
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    listMine().then(
      (r) => {
        if (r.gone.length) forgetTrails(r.gone);
        // A session that no longer opens anything is over.
        if (!r.email && trailSession()) setTrailSession(null);
        setEmail(r.email);
        setList(r.trails);
      },
      (e) => setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again.")),
    );
  }, [nonce, t]);

  const date = (s?: string) => (s ? new Date(s).toLocaleDateString(i18n.current(), { day: "numeric", month: "long" }) : "");
  const act = async (fn: () => Promise<unknown>, done?: string) => {
    setError(null);
    setNote(null);
    try {
      await fn();
      if (done) setNote(done);
      setNonce((n) => n + 1);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
    }
  };
  const stateText = (x: TrailSummary) =>
    ({
      draft: t("Draft · edited {{date}}", { date: date(x.updated_at) }),
      paid: t("Ready to play · start by {{date}}", { date: date(x.valid_until) }),
      playing: t("Playing until {{date}}", { date: date(x.valid_until) }),
      ended: t("Ended"),
    })[x.state];

  return (
    <>
      <section className="party-card">
        <h1>{t("My trails")}</h1>
        {email ? (
          <p className="party-muted">
            {t("Signed in as {{email}}.", { email })}{" "}
            <button
              type="button"
              className="party-link"
              onClick={() => {
                setTrailSession(null);
                setNonce((n) => n + 1);
              }}
            >
              {t("Sign out")}
            </button>
          </p>
        ) : null}
        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}
        {note && (
          <p className="party-note" role="status">
            {note}
          </p>
        )}
        {!list ? (
          <p className="party-muted">{t("Loading…")}</p>
        ) : list.length === 0 ? (
          <p className="party-muted">{t("No quiz trails here yet.")}</p>
        ) : (
          <ul className="party-trails">
            {list.map((x) => (
              <li key={x.id}>
                <div>
                  <strong>{x.title}</strong>
                  <small className="party-muted">
                    {t("{{n}} cards", { n: x.cards })} · {stateText(x)}
                    {!x.saved && " · " + t("only on this phone")}
                  </small>
                </div>
                <div className="party-actions">
                  {x.state !== "ended" && (
                    <a className="party-btn" href={editURL(x.id)}>
                      {x.state === "draft" ? t("Continue") : t("Edit")}
                    </a>
                  )}
                  {(x.state === "paid" || x.state === "playing") && (
                    <button type="button" className="party-btn" onClick={() => void act(() => sendHostLink(x.id), t("We've emailed you the link to the admin page. Older links stop working."))}>
                      {t("Email me the admin page")}
                    </button>
                  )}
                  <button
                    type="button"
                    className="party-btn"
                    onClick={() =>
                      void act(async () => {
                        const d = await duplicateDraft(x.id);
                        holdTrail({ id: d.id, secret: d.secret });
                      }, t("Copied. The copy is a new draft."))
                    }
                  >
                    {t("Duplicate")}
                  </button>
                  {x.state === "draft" && (
                    <button
                      type="button"
                      className="party-btn party-btn--danger"
                      onClick={() => {
                        if (confirm(t("Delete this quiz trail? This can't be undone."))) void act(() => deleteDraft(x.id).then(() => forgetTrails([x.id])));
                      }}
                    >
                      {t("Delete")}
                    </button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
      {!email && <SignIn />}
    </>
  );
}

/** Get a link by email: always "check your inbox", whether the address has trails or not. */
function SignIn() {
  const t = useT().t;
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <section className="party-card">
      <h2>{t("Made a quiz trail somewhere else?")}</h2>
      {sent ? (
        <p>{t("Check your inbox. If quiz trails are saved to {{email}}, the link to them is on its way.", { email })}</p>
      ) : (
        <form
          className="party-inline"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(null);
            try {
              await signIn(email.trim());
              setSent(true);
            } catch (err) {
              setError(err instanceof ApiError ? err.message : t("Something went wrong. Try again."));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label className="party-field">
            <span>{t("Get a link by email")}</span>
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder={t("Your email")} autoComplete="email" />
          </label>
          <button className="party-btn" disabled={busy}>
            {t("Send")}
          </button>
          {error && (
            <p className="party-error" role="alert">
              {error}
            </p>
          )}
        </form>
      )}
    </section>
  );
}

/** A mailed confirm or sign-in link lands here: the browser is signed in, then My trails. */
export function TrailVerify() {
  const t = useT().t;
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const token = location.hash.replace(/^#/, "");
    history.replaceState(null, "", location.pathname);
    verify(token).then(
      (s) => {
        setTrailSession(s);
        location.replace(MINE);
      },
      (e) => setError(e instanceof ApiError && e.status === 410 ? t("This link has expired or was used already. Ask for a new one.") : t("Something went wrong. Try again.")),
    );
  }, [t]);
  if (!error) return <p className="party-muted">{t("Loading…")}</p>;
  return (
    <section className="party-card">
      <h1>{t("That didn't work")}</h1>
      <p>{error}</p>
      <a className="party-btn" href={MINE}>
        {t("My trails")}
      </a>
    </section>
  );
}

/** Back from paying for a quiz trail: the admin link is in the mail. */
export function TrailPaid() {
  const t = useT().t;
  return (
    <section className="party-card">
      <h1>{t("Thank you — your quiz trail is ready to play")}</h1>
      <p className="party-lead">{t("We're emailing you the link to the admin page. There you print the cards and the answer key, and show the teams how to join.")}</p>
      <p className="party-muted">{t("It can take a minute or two. Check your spam folder if it doesn't arrive.")}</p>
      <a className="party-btn" href={MINE}>
        {t("My trails")}
      </a>
    </section>
  );
}
