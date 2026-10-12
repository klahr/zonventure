import { useEffect, useMemo, useState } from "react";
import { ApiError } from "@zonventure/api";
import { money } from "@zonventure/hosted";
import { useConfirm } from "@zonventure/ui";
import {
  Icon,
  problems,
  themeById,
  themeStyle,
  useTrailSave,
  type TrailApi,
  type TrailTheme,
} from "@zonventure/trail";
import { useT } from "./i18n.tsx";
import {
  checkout,
  setPaidHost,
  setTrailTheme,
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
import { leave, outside } from "./embed.ts";
import { Guide, GuideShell } from "./Guide.tsx";

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
          {t(
            "It may have been deleted, or it was made in another browser. Open it from the link in your email, or from My party adventures.",
          )}
        </p>
        <a className="party-btn" href={MINE} target={outside}>
          {t("My party adventures")}
        </a>
      </section>
    );
  }
  if (!doc) return <p className="party-muted">{t("Loading…")}</p>;
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
  const [ask, confirmDialog] = useConfirm();
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
  const [paying, setPaying] = useState(false);
  const [panel, setPanel] = useState<"email" | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
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
  const key =
    "id" in which ? heldTrails().find((k) => k.id === which.id) : undefined;
  const ended = live.state === "ended";

  const done = async () => {
    setBusy(true);
    try {
      if (!(await s.flush())) return;
      if (byHost) {
        leave(`/app/party/host/${encodeURIComponent(which.token)}`);
        return;
      }
      if (live.state !== "draft") {
        leave(MINE);
        return;
      }
      // Ready is reached only when every card is finished; this guards
      // a change made since.
      const first = problems(s.cards)[0];
      if (first) {
        setNote(
          first.card
            ? t("Finish the marked cards first.")
            : t("Add at least one question first."),
        );
        return;
      }
      setPaying(true);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (
      !("id" in which) ||
      !(await ask({
        message: t("Delete this party adventure? This can't be undone."),
        confirmLabel: t("Delete"),
        cancelLabel: t("Cancel"),
        danger: true,
      }))
    )
      return;
    try {
      await deleteDraft(which.id);
      leave(MINE);
    } catch (e) {
      setNote(
        e instanceof ApiError
          ? e.message
          : t("Something went wrong. Try again."),
      );
    }
  };

  const copy = async () => {
    if (!key) return;
    try {
      await navigator.clipboard.writeText(shareURL(key));
      setNote(t("Link copied. Anyone with it can edit this party adventure."));
    } catch {
      prompt(t("Copy this link:"), shareURL(key));
    }
  };

  if (paying && "id" in which)
    return (
      <GuideShell
        style={themeStyle(themeById(theme)?.look)}
        back={() => setPaying(false)}
      >
        <Checkout doc={live} id={which.id} cards={s.cards.length} />
      </GuideShell>
    );

  const saved = live.email
    ? t("Saved to {{email}}", { email: live.email })
    : byHost
      ? undefined
      : t("Saved on this phone");
  return (
    <>
      {confirmDialog}
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
        example={
          !byHost &&
          live.state === "draft" &&
          live.template !== "blank" &&
          live.revision === 0
        }
        finishOnOverview={byHost || live.state !== "draft"}
        freeTeams={live.price.free_teams}
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
            <button type="button" className="trail-btn" onClick={reload}>
              {t("Reload")}
            </button>
          )
        }
        notes={
          <>
            {live.round_open && (
              <p className="party-note">
                {t("A game is being played. Changes apply from the next game.")}
              </p>
            )}
            {live.reprint && live.state !== "draft" && (
              <p className="party-note">
                {t(
                  "The cards changed since you printed them. Print them again from the admin page before the next game.",
                )}
              </p>
            )}
            {ended && (
              <p className="party-note">
                {t("This party adventure has ended.")}
              </p>
            )}
          </>
        }
        extras={
          !byHost && (
            <div className="trail-guide__extras">
              {panel === "email" ? (
                <SaveToEmail close={() => setPanel(null)} />
              ) : (
                <ul className="trail-guide__actions">
                  {!live.email && (
                    <li>
                      <button type="button" onClick={() => setPanel("email")}>
                        <Icon name="mail" size={20} />
                        <span>
                          <strong>{t("Save to email")}</strong>
                          <small>
                            {t("Open it from any phone or computer.")}
                          </small>
                        </span>
                      </button>
                    </li>
                  )}
                  {key && (
                    <li>
                      <button type="button" onClick={() => void copy()}>
                        <Icon name="link" size={20} />
                        <strong>{t("Copy link")}</strong>
                      </button>
                    </li>
                  )}
                  <li>
                    <a href={MINE} target={outside}>
                      <Icon name="folder" size={20} />
                      <strong>{t("My party adventures")}</strong>
                    </a>
                  </li>
                  {live.state === "draft" && (
                    <li>
                      <button
                        type="button"
                        className="is-danger"
                        onClick={() => void remove()}
                      >
                        <Icon name="trash" size={20} />
                        <strong>{t("Delete")}</strong>
                      </button>
                    </li>
                  )}
                </ul>
              )}
            </div>
          )
        }
      />
    </>
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
          <p>
            {t(
              "We've sent you a link. Open it to save your party adventures to {{email}}.",
              { email },
            )}
          </p>
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
              setError(
                err instanceof ApiError
                  ? err.message
                  : t("Something went wrong. Try again."),
              );
            } finally {
              setBusy(false);
            }
          }}
        >
          <p className="party-muted">
            {t(
              "Then you can open your party adventures from any phone or computer. No password.",
            )}
          </p>
          <label className="party-field">
            <span>{t("Your email")}</span>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
            />
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
function Checkout({
  doc,
  id,
  cards,
}: {
  doc: PublicTrail;
  id: number;
  cards: number;
}) {
  const i18n = useT();
  const t = i18n.t;
  const loc = i18n.current();
  // No blocks: free, for up to free_teams teams a game.
  const [blocks, setBlocks] = useState(0);
  const [email, setEmail] = useState(trailSession()?.email ?? "");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const per = doc.price.teams_per_block;
  const limit = (b: number) => (b === 0 ? doc.price.free_teams : b * per);
  const total = doc.price.block_cents * blocks;
  const free = total <= 0;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await checkout(id, { email: email.trim(), blocks, consent });
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

  return (
    <section className="party-card">
      <p className="party-eyebrow">{t("Party adventure")}</p>
      <h1>{doc.title}</h1>
      <ul className="trail-guide__facts">
        <li>
          <Icon name="cards" size={20} />
          {t("{{n}} cards", { n: cards })}
        </li>
        <li>
          <Icon name="users" size={20} />
          {t("Up to {{n}} teams, one phone each", { n: limit(blocks) })}
        </li>
        <li>
          <Icon name="calendar" size={20} />
          {t("Play as often as you like for {{n}} days", { n: 14 })}
        </li>
      </ul>
      {/* Free play needs no payments; more teams do. */}
      <form className="party-form" onSubmit={submit}>
        {!doc.payments_open ? null : blocks === 0 ? (
          <button
            type="button"
            className="party-link"
            onClick={() => setBlocks(1)}
          >
            {t("More than {{n}} teams in one game?", {
              n: doc.price.free_teams,
            })}
          </button>
        ) : (
          <label className="party-field">
            <span>{t("Teams in one game")}</span>
            <div className="party-stepper">
              <button
                type="button"
                onClick={() => setBlocks((b) => Math.max(0, b - 1))}
                aria-label={t("Fewer")}
              >
                −
              </button>
              <output aria-live="polite">
                {t("up to {{n}}", { n: limit(blocks) })}
              </output>
              <button
                type="button"
                onClick={() => setBlocks((b) => Math.min(10, b + 1))}
                disabled={blocks >= 10}
                aria-label={t("More")}
              >
                +
              </button>
            </div>
          </label>
        )}
        <label className="party-field">
          <span>{t("Your email")}</span>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
          />
          <small className="party-muted">
            {free
              ? t(
                  "You go straight to the page you run it from. Sign in with this email to find it again.",
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
            </span>
          </label>
        )}
        {free ? (
          <div className="party-total">
            <span>{t("Total")}</span>
            <strong>{t("Free")}</strong>
          </div>
        ) : (
          <div className="party-total">
            <span>
              {blocks > 1
                ? t("{{price}} per {{n}} teams", {
                    price: money(
                      doc.price.block_cents,
                      doc.price.currency,
                      loc,
                    ),
                    n: per,
                  })
                : t("Total")}
            </span>
            <strong>{money(total, doc.price.currency, loc)}</strong>
          </div>
        )}
        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}
        <div className="party-actions">
          <button
            className="party-btn party-btn--primary"
            disabled={busy || !email || (doc.kids && !consent)}
          >
            {busy
              ? t("Opening checkout…")
              : free
                ? t("Get it ready to play")
                : t("Pay {{total}}", {
                    total: money(total, doc.price.currency, loc),
                  })}
          </button>
        </div>
      </form>
    </section>
  );
}
