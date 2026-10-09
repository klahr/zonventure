import { useEffect, useState } from "react";
import { ApiError, request } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import { coverOf } from "@zonventure/play-ui";
import { TEMPLATE_THEMES, TRAIL_THEMES, TemplatePicker, ThemeShowcase, themeById } from "@zonventure/trail";
import { SPOTS, useConfirm } from "@zonventure/ui";
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
  paidHost,
  setTrailTheme,
  sendHostLink,
  setPaidHost,
  setTrailSession,
  signIn,
  trailSession,
  verify,
  type TrailStart as Start,
  type TrailSummary,
} from "./trail.ts";
import { embedded, outside } from "./embed.ts";
import { Spot, ThankYouBanner } from "./pictures.tsx";

const MINE = "/app/party/trail/mine";

function MyTrailsLink() {
  const t = useT().t;
  // On the landing page, the page around the frame has the link.
  if (embedded || (heldTrails().length === 0 && !trailSession())) return null;
  return (
    <p className="party-right">
      <a href={MINE} target={outside}>
        {t("My party adventures")}
      </a>
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
  const [theme, setTheme] = useState(TRAIL_THEMES[0]!.id);
  const [picked, setPicked] = useState(false);

  useEffect(() => {
    getStart(org, lang).then(setStart, () => setMissing(true));
  }, [org, lang]);

  if (missing || (start && !start.enabled)) {
    return (
      <section className="party-card">
        <h1>{t("Not available")}</h1>
        <p>{t("Party adventures can't be made here.")}</p>
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
      // A look they chose is the one they get; otherwise the template's own.
      const th = themeById(picked ? theme : (TEMPLATE_THEMES[template] ?? theme));
      if (th) await setTrailTheme({ id: d.id }, { id: th.id, look: th.look }).catch(() => {});
      location.href = editURL(d.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
      setBusy(false);
    }
  };
  const price = start.price.block_cents > 0 ? money(start.price.block_cents, start.price.currency, i18n.current()) : null;
  const priceLine = (
    <p className="party-price">
      {price
        ? t("Free for up to {{free}} teams · {{price}} for up to {{n}}", { free: start.price.free_teams, price, n: start.price.teams_per_block })
        : t("Free to make and to play for {{days}} days", { days: start.play_days })}
    </p>
  );

  return (
    <>
      <MyTrailsLink />
      {/* On the landing page, the page around the frame says what it is. */}
      {!embedded && (
        <section className="party-card">
          <h1>{t("Make your own party adventure")}</h1>
          <p className="party-lead">{t("A little adventure for your party: write the questions, hide the printed cards around the house, the garden or the park, and let the teams set off with their phones. Who wins?")}</p>
          {priceLine}
        </section>
      )}
      <section className="party-card">
        <h2>{t("Start from")}</h2>
        {embedded && priceLine}
        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}
        <TemplatePicker i18n={i18n} templates={start.templates} busy={busy} onPick={(n) => void pick(n)} />
      </section>
      <section className="party-card">
        <h2>{t("Pick a look")}</h2>
        <ThemeShowcase
          i18n={i18n}
          value={theme}
          onChange={setTheme}
          onPicked={() => setPicked(true)}
          sample={{ text: t("How many legs does a spider have?"), options: ["6", "8", "10"], correct: "X" }}
        />
      </section>
    </>
  );
}

/** My trails: this browser's, and every one saved to the signed-in email. */
export function TrailMine() {
  const i18n = useT();
  const t = i18n.t;
  const [ask, confirmDialog] = useConfirm();
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
      {confirmDialog}
      <section className="party-card">
        <h1>{t("My party adventures")}</h1>
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
          <div className="party-empty">
            <Spot src={SPOTS.noTrails} />
            <p className="party-muted">{t("No party adventures here yet.")}</p>
          </div>
        ) : (
          <ul className="party-trails">
            {list.map((x) => (
              <li key={x.id}>
                <div className="party-trails__head">
                  <span
                    className="party-trails__thumb"
                    style={{ backgroundColor: themeById(x.theme)?.look.accent, backgroundImage: coverOf(x.theme) ? `url(${coverOf(x.theme)!.small})` : undefined }}
                    aria-hidden="true"
                  />
                  <div>
                    <strong>{x.title}</strong>
                    <small className="party-muted">
                      {t("{{n}} cards", { n: x.cards })} · {stateText(x)}
                      {!x.saved && " · " + t("only on this phone")}
                    </small>
                  </div>
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
                      onClick={async () => {
                        if (await ask({ message: t("Delete this party adventure? This can't be undone."), confirmLabel: t("Delete"), cancelLabel: t("Cancel"), danger: true }))
                          void act(() => deleteDraft(x.id).then(() => forgetTrails([x.id])));
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
      <h2>{t("Made a party adventure somewhere else?")}</h2>
      {sent ? (
        <p>{t("Check your inbox. If party adventures are saved to {{email}}, the link to them is on its way.", { email })}</p>
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
        {t("My party adventures")}
      </a>
    </section>
  );
}

/** How long the thank-you page waits for the payment to land. */
const PAID_WAIT_MS = 60_000;

/**
 * Back from paying for a quiz trail. The browser that paid holds the
 * admin page's link (from checkout): once the payment has landed the
 * page opens, so the buyer goes straight there. The link is in the mail
 * too, for any other device.
 */
export function TrailPaid() {
  const t = useT().t;
  const [held] = useState(() => {
    const p = paidHost();
    return p && Date.now() - p.at < 24 * 3600_000 ? p.url : null;
  });
  const [ready, setReady] = useState(false);
  const [late, setLate] = useState(false);

  useEffect(() => {
    if (!held) return;
    const token = decodeURIComponent(held.slice(held.lastIndexOf("/") + 1));
    const started = Date.now();
    let stop = false;
    const look = async () => {
      if (stop) return;
      try {
        await request(`/api/party/${encodeURIComponent(token)}`);
        setPaidHost(null);
        setReady(true);
        location.replace(held);
        return;
      } catch {
        // Not paid yet: the payment's webhook has not landed.
      }
      if (Date.now() - started > PAID_WAIT_MS) setLate(true);
      setTimeout(() => void look(), 2000);
    };
    void look();
    return () => {
      stop = true;
    };
  }, [held]);

  return (
    <section className="party-card party-card--cover">
      <ThankYouBanner />
      <h1>{t("Thank you — your party adventure is ready to play")}</h1>
      {held && !late ? (
        <p className="party-lead" role="status">
          {ready ? t("Opening your admin page…") : t("Getting your admin page ready…")}
        </p>
      ) : (
        <>
          {held && (
            <p>
              <a className="party-btn party-btn--primary" href={held}>
                {t("Open the admin page")}
              </a>
            </p>
          )}
          <p className="party-lead">{t("We're emailing you the link to the admin page. There you print the cards and the answer key, and show the teams how to join.")}</p>
        </>
      )}
      <p className="party-muted">{t("It can take a minute or two. Check your spam folder if it doesn't arrive.")}</p>
      <a className="party-btn" href={MINE}>
        {t("My party adventures")}
      </a>
    </section>
  );
}
