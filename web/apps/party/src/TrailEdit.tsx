import { useEffect, useMemo, useState } from "react";
import { ApiError } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import {
  Icon,
  useTrailSave,
  type TrailApi,
  type TrailTheme,
} from "@zonventure/trail";
import { useConfirm } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import {
  checkout,
  lastCheckout,
  setLastCheckout,
  setPaidHost,
  setTrailTheme,
  getDraft,
  getHostTrail,
  holdTrail,
  saveDraft,
  saveHostTrail,
  trailSession,
  type PublicTrail,
} from "./trail.ts";
import { leave, outside } from "./embed.ts";
import { Guide } from "./Guide.tsx";
import { PrivateWarning } from "./TrailPages.tsx";

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
 * The public creator: a guide through the same cards as the console's
 * editor, saved to the draft (or, from the host page, to the trail the host link runs). Done
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
    const load =
      "token" in which ? getHostTrail(which.token) : getDraft(which.id);
    load.then(setDoc, (e) => {
      if (e instanceof ApiError && e.status === 404) setMissing(true);
      else setMissing(true);
    });
  }, [which, nonce]);

  if (missing || !which) {
    return (
      <section className="party-card">
        <h1>{t("This party adventure can't be found")}</h1>
        <p>
          {host
            ? t(
                "It may have been deleted, or it was made in another browser. Open it from the link in your email, or from My party adventures.",
              )
            : t(
                "It may have been deleted, or it was made in another browser. Open it in the browser you made it in, or from My party adventures.",
              )}
        </p>
        <a className="zv-btn" href={MINE} target={outside}>
          {t("My party adventures")}
        </a>
      </section>
    );
  }
  if (!doc) return <p className="zv-muted">{t("Loading…")}</p>;
  return (
    <Creator
      key={doc.revision + ":" + nonce}
      doc={doc}
      which={which}
      reload={() => setNonce((n) => n + 1)}
    />
  );
}

function Creator({
  doc,
  which,
  reload,
}: {
  doc: PublicTrail;
  which: { id: number } | { token: string };
  reload: () => void;
}) {
  const i18n = useT();
  const t = i18n.t;
  const byHost = "token" in which;
  const api = useMemo<TrailApi>(
    () => ({
      save: (body) =>
        "token" in which
          ? saveHostTrail(which.token, body)
          : saveDraft(which.id, body),
    }),
    [which],
  );
  const s = useTrailSave(api, doc);
  const live = s.doc as PublicTrail;
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [ask, confirmDialog] = useConfirm();
  const [theme, setTheme] = useState(doc.theme);
  const pickTheme = async (th: TrailTheme) => {
    const was = theme;
    setTheme(th.id);
    try {
      await setTrailTheme(which, { id: th.id, look: th.look });
    } catch (e) {
      setTheme(was);
      setNote(
        e instanceof ApiError
          ? e.message
          : t("Something went wrong. Try again."),
      );
    }
  };
  const ended = live.state === "ended";

  const done = async () => {
    setBusy(true);
    try {
      if (!(await s.flush())) return;
      if (byHost) {
        leave(`/app/party/host/${encodeURIComponent(which.token)}`);
        return;
      }
      leave(MINE);
    } finally {
      setBusy(false);
    }
  };

  const ready = useCheckout(
    live,
    "id" in which ? which.id : 0,
    s.flush,
    s.cards.filter((c) => c.example).length,
  );

  // Plain "Saved" until checkout gives it an email to say.
  const saved = live.email
    ? t("Saved to {{email}}", { email: live.email })
    : undefined;
  return (
    <Guide
      title={s.title}
      cards={s.cards}
      state={s.state}
      readOnly={s.readOnly || ended}
      onChange={s.change}
      savedText={saved}
      theme={theme}
      onTheme={ended ? undefined : (th) => void pickTheme(th)}
      fresh={!byHost && live.state === "draft" && live.revision === 0}
      finishOnOverview={byHost || live.state !== "draft"}
      ready={ready}
      note={note}
      close={() =>
        void s
          .flush()
          .then(() =>
            leave(
              byHost
                ? `/app/party/host/${encodeURIComponent(which.token)}`
                : MINE,
            ),
          )
      }
      finish={() => void done()}
      busy={busy}
      barAction={
        s.state === "conflict" && (
          <>
            <button
              type="button"
              className="trail-btn"
              onClick={async () => {
                if (
                  await ask({
                    message: t("Reloading shows the version saved elsewhere. Your latest change here wasn't saved and will be lost."),
                    confirmLabel: t("Reload"),
                    cancelLabel: t("Cancel"),
                    danger: true,
                  })
                )
                  reload();
              }}
            >
              {t("Reload")}
            </button>
            {confirmDialog}
          </>
        )
      }
      notes={
        <>
          {!byHost && <PrivateWarning />}
          {live.round_open && (
            <p className="zv-note">
              {t("A game is being played. Changes apply from the next game.")}
            </p>
          )}
          {live.reprint && live.state !== "draft" && (
            <p className="zv-note">
              {t(
                "The cards changed since you printed them. Print them again from the host page before the next game.",
              )}
            </p>
          )}
          {ended && (
            <p className="zv-note">{t("This party adventure has ended.")}</p>
          )}
        </>
      }
    />
  );
}

const CHECKOUT_FORM = "trail-checkout";
/** The most teams the stepper offers; the server takes up to 150. */
const MAX_TEAMS = 50;

/**
 * The guide's last screen: how many teams (free up to the free number;
 * more only while payments are open), the email the host link goes to,
 * a guardian's consent for a children's trail, and what comes next. The
 * button that sends it sits in the guide's foot, so the two come back
 * apart. Sending saves the cards first, and asks first while cards are
 * still the template's examples.
 */
function useCheckout(
  doc: PublicTrail,
  id: number,
  flush: () => Promise<boolean>,
  examples: number,
): { body: React.ReactNode; action: React.ReactNode } {
  const i18n = useT();
  const t = i18n.t;
  const loc = i18n.current();
  const [ask, confirmDialog] = useConfirm();
  // Up to free_teams teams a game are free. Back from a cancelled
  // payment, what was asked for then.
  const [last] = useState(() => lastCheckout(id));
  const minTeams = doc.price.free_teams;
  const [teams, setTeams] = useState(Math.max(minTeams, last?.teams ?? 0));
  const [email, setEmail] = useState(trailSession()?.email ?? last?.email ?? "");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const total = doc.price.team_cents * Math.max(0, teams - minTeams);
  const free = total <= 0;
  const price = money(total, doc.price.currency, loc);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (
      examples > 0 &&
      !(await ask({
        message:
          examples === 1
            ? t("1 card is still an example, with the template's question and answer. Use it anyway?")
            : t("{{n}} cards are still examples, with the template's questions and answers. Use them anyway?", { n: examples }),
        confirmLabel: examples === 1 ? t("Use it anyway") : t("Use them anyway"),
        cancelLabel: t("Cancel"),
      }))
    )
      return;
    setBusy(true);
    setError(null);
    try {
      if (!(await flush())) throw new Error();
      setLastCheckout({ id, email: email.trim(), teams });
      const r = await checkout(id, { email: email.trim(), teams, consent });
      if (r.checkout_url) {
        setPaidHost(r.host_url ?? null);
        leave(r.checkout_url);
      } else if (r.host_url) leave(r.host_url);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : t("Something went wrong. Try again."),
      );
      setBusy(false);
    }
  };

  const body = (
    <form
      id={CHECKOUT_FORM}
      className="trail-guide__checkout"
      onSubmit={submit}
    >
      {confirmDialog}
      <div className="trail-guide__panel">
        <div className="trail-guide__teams">
          <Icon name="users" size={22} />
          <span className="trail-guide__teams-label">
            <strong>{t("Teams")}</strong>
            <small>{t("One phone each")}</small>
          </span>
          {/* Always there, so more teams are never a mystery; + waits
              for payments to be open. */}
          <div className="party-stepper">
            <button
              type="button"
              onClick={() => setTeams((n) => Math.max(minTeams, n - 1))}
              disabled={teams <= minTeams}
              aria-label={t("Fewer")}
            >
              −
            </button>
            <output aria-live="polite">
              {t("up to {{n}}", { n: teams })}
            </output>
            <button
              type="button"
              onClick={() => setTeams((n) => Math.min(MAX_TEAMS, n + 1))}
              disabled={!doc.payments_open || teams >= MAX_TEAMS}
              aria-label={t("More")}
            >
              +
            </button>
          </div>
          {doc.payments_open ? (
            doc.price.team_cents > 0 && (
              <small className="trail-guide__teams-note">
                {t("Free for up to {{free}} teams, then {{price}} per team.", {
                  free: minTeams,
                  price: money(doc.price.team_cents, doc.price.currency, loc),
                })}
              </small>
            )
          ) : (
            <small className="trail-guide__teams-note">
              {t("More teams can't be bought right now.")}
            </small>
          )}
          <strong className="trail-guide__price">
            {free ? t("Free") : price}
          </strong>
        </div>
        <label className="zv-field">
          <span>{t("Your email")}</span>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
          />
          <small className="zv-muted">
            {free
              ? t(
                  "You go straight to the page you run it from, and we email you the link to it.",
                )
              : t("We send the link you run the party adventure from here.")}
          </small>
        </label>
        {doc.kids && (
          <label className="party-check">
            <input
              type="checkbox"
              checked={consent}
              onChange={(e) => setConsent(e.target.checked)}
              required
            />
            <span>
              {t(
                "I am the guardian of the children who will play, or I have their guardians' consent.",
              )}
              <small className="zv-muted">
                {t("We ask because children play: the game keeps their team names and answers for a while.")}
              </small>
            </span>
          </label>
        )}
        {error && (
          <p className="zv-error" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="trail-guide__then">
        <p>{t("Then")}</p>
        <ol>
          <li>
            <Icon name="print" size={20} />
            {t("Print the cards")}
          </li>
          <li>
            <Icon name="hide" size={20} />
            {t("Hide them")}
          </li>
          <li>
            <Icon name="phone" size={20} />
            {t("Play on your phones")}
          </li>
        </ol>
        <p className="trail-guide__then-note">
          {t("Play as often as you like for {{n}} days", { n: 14 })}
        </p>
      </div>
    </form>
  );
  const action = (
    <button
      type="submit"
      form={CHECKOUT_FORM}
      className="trail-btn trail-btn--primary trail-guide__next"
      disabled={busy}
    >
      {busy
        ? t("Opening checkout…")
        : free
          ? t("Get it ready – free")
          : t("Pay {{total}}", { total: price })}
    </button>
  );
  return { body, action };
}
