import { useEffect, useState } from "react";
import { ApiError, request } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import { coverOf } from "@zonventure/play-ui";
import { Icon, TRAIL_THEMES, TemplatePicker, themeById, themeStyle, type TrailTheme } from "@zonventure/trail";
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
  hostLink,
  lastCheckout,
  listMine,
  paidHost,
  privateWindow,
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
import { embedded, leave, outside } from "./embed.ts";
import { GuideShell, ThemeHero } from "./Guide.tsx";
import { Spot, ThankYouBanner } from "./pictures.tsx";

const MINE = "/app/party/trail/mine";

/** A word when this looks like a private window: closing it loses the party adventures it holds. */
export function PrivateWarning() {
  const t = useT().t;
  const [shown, setShown] = useState(false);
  useEffect(() => {
    void privateWindow().then(setShown);
  }, []);
  if (!shown) return null;
  return (
    <p className="zv-note" role="status">
      {t(
        "This looks like a private window: what you make here is kept only until you close it. Copy the link from the address bar, or once your party adventure is ready, use \"Email me the host page\" under My party adventures.",
      )}
    </p>
  );
}

function MyTrailsLink() {
  const t = useT().t;
  // On the landing page, the page around the frame has the link.
  if (embedded) return null;
  return (
    <p className="party-right">
      <a href={MINE} target={outside}>
        {t("My party adventures")}
      </a>
    </p>
  );
}

/** The looks the start page shows first; "More themes" shows them all. */
const FAVOURITES = ["party", "summer", "christmas", "halloween", "jungle", "pirate", "space", "office"];

const param = (name: string) => new URLSearchParams(location.search).get(name);

/**
 * Start: pick a theme, then, fullscreen, who it is for (a template with
 * example cards, or blank) and, for a template about someone, whose
 * birthday it is. The draft is made only then. Each step is in the
 * address (?theme=, &for=), so back returns to the one before and, from
 * the landing page's frame, the guide opens out of it.
 */
export function TrailStart({ org }: { org: string }) {
  const i18n = useT();
  const t = i18n.t;
  const lang = i18n.current().startsWith("sv") ? "sv" : "en";
  const [start, setStart] = useState<Start | null>(null);
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [chosen, setChosen] = useState(() => param("theme"));
  const [forWhom, setForWhom] = useState(() => param("for"));
  const [more, setMore] = useState(false);
  const [person, setPerson] = useState("");
  const [age, setAge] = useState("");

  useEffect(() => {
    getStart(org, lang).then(setStart, () => setMissing(true));
  }, [org, lang]);
  useEffect(() => {
    const pop = () => {
      setChosen(param("theme"));
      setForWhom(param("for"));
    };
    addEventListener("popstate", pop);
    return () => removeEventListener("popstate", pop);
  }, []);

  if (missing || (start && !start.enabled)) {
    return (
      <section className="party-card">
        <h1>{t("Not available")}</h1>
        <p>{t("Party adventures can't be made here.")}</p>
      </section>
    );
  }
  if (!start) return <p className="zv-muted">{t("Loading…")}</p>;

  const theme = themeById(chosen ?? undefined);
  const step = (url: string) => {
    history.pushState(null, "", url);
    setChosen(param("theme"));
    setForWhom(param("for"));
    scrollTo(0, 0);
  };
  const choose = (th: TrailTheme) => {
    const url = `${location.pathname}?theme=${encodeURIComponent(th.id)}`;
    if (embedded) return leave(url);
    step(url);
  };
  const make = async (template: string, who?: { name: string; age: number }) => {
    if (!theme) return;
    setBusy(true);
    setError(null);
    try {
      const d = await createDraft(org, template, lang, who);
      holdTrail({ id: d.id, secret: d.secret });
      await setTrailTheme({ id: d.id }, { id: theme.id, look: theme.look }).catch(() => {});
      // In place of this step, so back never offers to make it again.
      location.replace(editURL(d.id));
    } catch (e) {
      setError(e instanceof ApiError ? i18n.tError(e.code, e.message) : t("Something went wrong. Try again."));
      setBusy(false);
    }
  };

  const template = start.templates.find((tp) => tp.name === forWhom && tp.person);
  if (theme && template) {
    const n = Number(age);
    return (
      <GuideShell
        style={themeStyle(theme.look)}
        back={() => history.back()}
        hero={<ThemeHero theme={theme} />}
        foot={
          <button
            type="button"
            className="trail-btn trail-btn--primary trail-guide__next"
            disabled={busy || !person.trim()}
            onClick={() => void make(template.name, { name: person.trim(), age: n > 0 ? n : 0 })}
          >
            {t("Next")}
            <Icon name="right" size={18} />
          </button>
        }
      >
        <h1 className="trail-guide__title">{t("Whose birthday is it?")}</h1>
        <p className="trail-guide__lead">{t("We put the name and the age in the example questions.")}</p>
        {error && (
          <p className="zv-error" role="alert">
            {error}
          </p>
        )}
        <div className="trail-guide__person">
          <label className="zv-field">
            <span>{t("Name")}</span>
            <input value={person} maxLength={40} autoFocus autoComplete="off" placeholder="Elsa" onChange={(e) => setPerson(e.target.value)} />
          </label>
          <label className="zv-field trail-guide__age">
            <span>{t("Turns")}</span>
            <input type="number" inputMode="numeric" min={1} max={120} value={age} placeholder="8" onChange={(e) => setAge(e.target.value)} />
          </label>
        </div>
        <button type="button" className="zv-link trail-link trail-guide__skip" disabled={busy} onClick={() => void make(template.name)}>
          {t("Skip, keep the example")}
        </button>
      </GuideShell>
    );
  }

  if (theme) {
    return (
      <GuideShell style={themeStyle(theme.look)} back={() => history.back()} hero={<ThemeHero theme={theme} onChange={() => history.back()} />}>
        <h1 className="trail-guide__title">{t("Who is it for?")}</h1>
        <p className="trail-guide__lead">{t("You get example cards to make your own.")}</p>
        {error && (
          <p className="zv-error" role="alert">
            {error}
          </p>
        )}
        <TemplatePicker
          i18n={i18n}
          templates={start.templates}
          busy={busy}
          themed
          onPick={(name) =>
            start.templates.find((tp) => tp.name === name)?.person
              ? step(`${location.pathname}?theme=${encodeURIComponent(theme.id)}&for=${encodeURIComponent(name)}`)
              : void make(name)
          }
        />
      </GuideShell>
    );
  }

  const price = start.price.team_cents > 0 ? money(start.price.team_cents, start.price.currency, i18n.current()) : null;
  const priceLine = (
    <p className="trail-guide__chip">
      <Icon name="users" size={18} />
      {price
        ? t("{{free}} teams free · {{price}} per team after", { free: start.price.free_teams, price })
        : t("Free to play for {{days}} days", { days: start.play_days })}
    </p>
  );
  const shown = more ? TRAIL_THEMES : TRAIL_THEMES.filter((th) => FAVOURITES.includes(th.id));

  return (
    <>
      <MyTrailsLink />
      {/* On the landing page, the page around the frame says what it is. */}
      {!embedded && (
        <section className="party-card">
          <h1>{t("Make your own party adventure")}</h1>
          <p className="party-lead">{t("A little adventure for your party: write the questions, put the printed cards out along a trail in the garden or the park, and let the teams set off with their phones. Who wins?")}</p>
        </section>
      )}
      <section className="party-card">
        <h2>{t("Pick a theme")}</h2>
        <p className="zv-muted">{t("The look of the cards and the phones. You can change it later.")}</p>
        {priceLine}
        <ul className="trail-guide-themes">
          {shown.map((th) => {
            const cover = coverOf(th.id);
            return (
              <li key={th.id}>
                <button
                  type="button"
                  className="trail-guide-theme"
                  style={{ ...themeStyle(th.look), ...(cover ? { backgroundImage: `url(${cover.small})` } : {}) }}
                  onClick={() => choose(th)}
                >
                  <span>{t(th.label)}</span>
                </button>
              </li>
            );
          })}
        </ul>
        {!more && (
          <button type="button" className="zv-btn trail-guide-themes__more" onClick={() => setMore(true)}>
            {t("More themes")}
          </button>
        )}
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
      setError(e instanceof ApiError ? i18n.tError(e.code, e.message) : t("Something went wrong. Try again."));
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
          <p className="zv-muted">
            {t("Signed in as {{email}}.", { email })}{" "}
            <button
              type="button"
              className="zv-link party-link"
              onClick={() => {
                setTrailSession(null);
                setNonce((n) => n + 1);
              }}
            >
              {t("Sign out")}
            </button>
          </p>
        ) : null}
        <PrivateWarning />
        {error && (
          <p className="zv-error" role="alert">
            {error}
          </p>
        )}
        {note && (
          <p className="zv-note" role="status">
            {note}
          </p>
        )}
        {!list ? (
          <p className="zv-muted">{t("Loading…")}</p>
        ) : list.length === 0 ? (
          <div className="party-empty">
            <Spot src={SPOTS.noTrails} />
            <p className="zv-muted">{t("No party adventures here yet.")}</p>
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
                    <small className="zv-muted">
                      {t("{{n}} cards", { n: x.cards })} · {stateText(x)}
                      {!x.saved && " · " + t("only in this browser")}
                    </small>
                  </div>
                </div>
                <div className="party-actions">
                  {x.state !== "ended" && (
                    <a className="zv-btn" href={editURL(x.id)}>
                      {x.state === "draft" ? t("Continue") : t("Edit")}
                    </a>
                  )}
                  {(x.state === "paid" || x.state === "playing") && hostLink(x.id) && (
                    <a className="zv-btn zv-btn--primary" href={"/app/party/host/" + encodeURIComponent(hostLink(x.id)!)}>
                      {t("Open the host page")}
                    </a>
                  )}
                  {(x.state === "paid" || x.state === "playing") && (
                    <button type="button" className="zv-btn" onClick={() => void act(() => sendHostLink(x.id), t("We've emailed you the link to the host page."))}>
                      {t("Email me the host page")}
                    </button>
                  )}
                  <button
                    type="button"
                    className="zv-btn"
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
                      className="zv-btn zv-btn--danger"
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
  const i18n = useT();
  const t = i18n.t;
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
          className="zv-inline"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(null);
            try {
              await signIn(email.trim());
              setSent(true);
            } catch (err) {
              setError(err instanceof ApiError ? i18n.tError(err.code, err.message) : t("Something went wrong. Try again."));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label className="zv-field">
            <span>{t("Get a link by email")}</span>
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder={t("Your email")} autoComplete="email" />
          </label>
          <button className="zv-btn" disabled={busy}>
            {t("Send")}
          </button>
          {error && (
            <p className="zv-error" role="alert">
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
  if (!error) return <p className="zv-muted">{t("Loading…")}</p>;
  return (
    <section className="party-card">
      <h1>{t("That didn't work")}</h1>
      <p>{error}</p>
      <a className="zv-btn" href={MINE}>
        {t("My party adventures")}
      </a>
    </section>
  );
}

/** How long the thank-you page waits for the payment to land. */
const PAID_WAIT_MS = 60_000;

/**
 * Back from paying for a quiz trail. The browser that paid holds the
 * host page's link (from checkout): once the payment has landed the
 * page opens, so the buyer goes straight there. The link is in the mail
 * too, for any other device.
 */
export function TrailPaid() {
  const t = useT().t;
  const cancelled = new URLSearchParams(location.search).has("cancelled");
  const [held] = useState(() => {
    const p = paidHost();
    return !cancelled && p && Date.now() - p.at < 24 * 3600_000 ? p.url : null;
  });
  // Back from a cancelled payment: the draft it was for, to try again from.
  const [retry] = useState(() => (cancelled ? heldTrails().find((k) => lastCheckout(k.id)) : undefined));
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

  if (cancelled) {
    return (
      <section className="party-card">
        <h1>{t("The payment was cancelled. Nothing was charged.")}</h1>
        {retry && (
          <p>
            <a className="zv-btn zv-btn--primary" href={editURL(retry.id)}>
              {t("Back to your party adventure")}
            </a>
          </p>
        )}
        <a className="zv-btn" href={MINE}>
          {t("My party adventures")}
        </a>
      </section>
    );
  }
  return (
    <section className="party-card party-card--cover">
      <ThankYouBanner />
      <h1>{t("Thank you — your party adventure is ready to play")}</h1>
      {held && !late ? (
        <p className="party-lead" role="status">
          {ready ? t("Opening your host page…") : t("Getting your host page ready…")}
        </p>
      ) : (
        <>
          {held && (
            <p>
              <a className="zv-btn zv-btn--primary" href={held}>
                {t("Open the host page")}
              </a>
            </p>
          )}
          <p className="party-lead">{t("We're emailing you the link to the host page. There you print the cards and the answer key, and show the teams how to join.")}</p>
        </>
      )}
      <p className="zv-muted">{t("It can take a minute or two. Check your spam folder if it doesn't arrive.")}</p>
      <a className="zv-btn" href={MINE}>
        {t("My party adventures")}
      </a>
    </section>
  );
}
