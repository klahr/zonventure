import { useEffect, useRef, useState, type ReactNode } from "react";
import type { play } from "@zonventure/api";
import { Markdown } from "./Markdown.tsx";
import { formatSeconds, secondsUntil, useEngineClock } from "./clock.ts";
import { english, type Feedback, type Translate } from "./feedback.ts";

/**
 * One step of an adventure, as a player sees it.
 *
 * This is the only renderer of a View. The player app shows the live
 * game with it; the designer's preview and simulator show the draft with
 * it. An author therefore previews the exact screen, wording and
 * behaviour a team will get — there is no "preview version" to drift.
 *
 * It is controlled: it holds only the answer being typed and whether a
 * confirmation is open. Everything else comes from the view, and every
 * action goes out through onAction for the caller to send. It never
 * moves on by itself — a wrong optimistic advance in a puzzle hunt shows
 * the next clue to a team that has not earned it.
 */
export interface PlayViewProps {
  view: play.View;
  onAction: (action: play.PlayAction) => void;
  /** An action is in flight; buttons are disabled. */
  busy?: boolean;
  feedback?: Feedback | null;
  /**
   * Whether the clock runs. True in the player app; false in the
   * simulator, where time moves only when the author advances it.
   */
  live?: boolean;
  t?: Translate;
  /** Extra line under a zone requirement, such as the distance to it. */
  zoneNote?: ReactNode;
}

export function PlayView(props: PlayViewProps) {
  const { view } = props;
  const t = props.t ?? english;
  const now = useEngineClock(view.now, props.live ?? true);
  // Keyed on the node so typing, open confirmations and one-shot timers
  // reset when the team moves on.
  const key = view.node ?? "none";

  let body: ReactNode;
  if (view.finished || view.type === "end") body = <EndStep {...props} t={t} />;
  else if (view.type === "puzzle") body = <PuzzleStep key={key} {...props} t={t} now={now} />;
  else if (view.type === "story") body = <StoryStep key={key} {...props} t={t} />;
  else if (view.type === "hub") body = <HubStep key={key} {...props} t={t} />;
  else body = <p className="pv-muted">{t("Moving on…")}</p>;

  const net = view.score - view.penalty_score;
  return (
    <div className="pv" data-theme-name={view.theme || undefined}>
      <header className="pv-head">
        {view.title && <h2 className="pv-title">{view.title}</h2>}
        <span className="pv-score" aria-label={t("Score")}>
          {t("{{points}} pts", { points: round(net) })}
        </span>
      </header>
      <FeedbackBanner feedback={props.feedback ?? null} />
      {body}
      <Bag view={view} t={t} />
    </div>
  );
}

function FeedbackBanner({ feedback }: { feedback: Feedback | null }) {
  // Always rendered, so a screen reader announces each new message.
  return (
    <div className={"pv-feedback" + (feedback ? ` pv-feedback--${feedback.tone}` : "")} role="status" aria-live="polite">
      {feedback?.text}
    </div>
  );
}

interface StepProps extends PlayViewProps {
  t: Translate;
}

function ZoneBanner({ view, t, note }: { view: play.View; t: Translate; note?: ReactNode }) {
  const z = view.zone;
  if (!z) return null;
  const name = z.name || t("the marked place");
  const map = `https://www.openstreetmap.org/?mlat=${z.lat}&mlon=${z.lng}#map=18/${z.lat}/${z.lng}`;
  return (
    <div className={"pv-zone" + (view.here ? " is-here" : "")}>
      <strong>{view.here ? t("You are at {{place}}.", { place: name }) : t("Go to {{place}}.", { place: name })}</strong>
      {!view.here && note && <span className="pv-zone__note">{note}</span>}
      <a href={map} target="_blank" rel="noopener noreferrer">
        {t("Show on map")}
      </a>
    </div>
  );
}

function PuzzleStep(props: StepProps & { now: number }) {
  const { view, t, now, busy, onAction } = props;
  const node = view.node ?? "";
  const [guess, setGuess] = useState("");
  const [confirm, setConfirm] = useState<"hint" | "skip" | null>(null);
  const live = props.live ?? true;

  const blockedFor = secondsUntil(view.blocked_until, now);
  const timeLeft = view.deadline ? secondsUntil(view.deadline, now) : Infinity;
  const timeUp = timeLeft <= 0;
  const needsPlace = !!view.zone && !view.here;

  // Live, the client calls Timeout itself when the countdown ends — the
  // engine re-checks the time, so an early or repeated call is harmless.
  const firedTimeout = useRef(false);
  useEffect(() => {
    if (live && timeUp && view.has_timeout && !firedTimeout.current) {
      firedTimeout.current = true;
      onAction({ kind: "timeout", node });
    }
  }, [live, timeUp, view.has_timeout, node, onAction]);

  const submit = (e: { preventDefault(): void }) => {
    e.preventDefault();
    const g = guess.trim();
    if (!g || busy) return;
    onAction({ kind: "answer", node, guess: g });
  };

  const hint = view.next_hint;
  const hintIn = hint ? secondsUntil(hint.available_at, now) : 0;
  const input = view.input ?? { kind: "text" as const };

  return (
    <>
      <ZoneBanner view={view} t={t} note={props.zoneNote} />
      {view.text && <Markdown className="pv-text" text={view.text} />}

      {view.deadline && (
        <p className={"pv-timer" + (timeLeft < 60 ? " is-urgent" : "")}>
          {timeUp ? t("Time is up.") : t("Time left: {{time}}", { time: formatSeconds(timeLeft) })}
        </p>
      )}
      {timeUp && view.has_timeout && !live && (
        <button type="button" className="pv-btn" disabled={busy} onClick={() => onAction({ kind: "timeout", node })}>
          {t("Continue")}
        </button>
      )}

      {!timeUp && (
        <form className="pv-answer" onSubmit={submit}>
          <label className="pv-label" htmlFor="pv-guess">
            {input.parts
              ? t(input.ordered ? "Your answer: {{n}} things, in order, separated by commas" : "Your answer: {{n}} things, separated by commas", { n: input.parts })
              : t("Your answer")}
          </label>
          <div className="pv-answer__row">
            <input
              id="pv-guess"
              className="pv-input"
              value={guess}
              onChange={(e) => setGuess(e.target.value)}
              inputMode={input.kind === "number" ? "decimal" : "text"}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              enterKeyHint="send"
              disabled={needsPlace}
            />
            <button className="pv-btn pv-btn--primary" disabled={busy || !guess.trim() || blockedFor > 0 || needsPlace}>
              {t("Answer")}
            </button>
          </div>
          {blockedFor > 0 && <p className="pv-muted">{t("Try again in {{time}}", { time: formatSeconds(blockedFor) })}</p>}
          {needsPlace && <p className="pv-muted">{t("You can answer when you are there.")}</p>}
        </form>
      )}

      {(view.hints?.length || hint) && (
        <section className="pv-hints" aria-label={t("Hints")}>
          {view.hints?.map((h, i) => (
            <div key={i} className="pv-hint">
              <span className="pv-hint__n">{t("Hint {{n}}", { n: i + 1 })}</span>
              <Markdown text={h} />
            </div>
          ))}
          {hint &&
            (hintIn > 0 ? (
              <p className="pv-muted">{t("Next hint in {{time}}", { time: formatSeconds(hintIn) })}</p>
            ) : confirm === "hint" ? (
              <Confirm
                t={t}
                text={t("This hint costs {{n}} points.", { n: hint.cost ?? 0 })}
                busy={busy}
                onYes={() => {
                  setConfirm(null);
                  onAction({ kind: "hint", node, index: hint.index });
                }}
                onNo={() => setConfirm(null)}
              />
            ) : (
              <button
                type="button"
                className="pv-btn"
                disabled={busy}
                onClick={() => (hint.cost ? setConfirm("hint") : onAction({ kind: "hint", node, index: hint.index }))}
              >
                {hint.cost ? t("Get a hint (−{{n}} points)", { n: hint.cost }) : t("Get a hint")}
              </button>
            ))}
        </section>
      )}

      {view.can_skip &&
        (confirm === "skip" ? (
          <Confirm
            t={t}
            text={t("Skip this puzzle? You won't get its points.")}
            busy={busy}
            onYes={() => {
              setConfirm(null);
              onAction({ kind: "skip", node });
            }}
            onNo={() => setConfirm(null)}
          />
        ) : (
          <button type="button" className="pv-link" disabled={busy} onClick={() => setConfirm("skip")}>
            {t("Skip this puzzle")}
          </button>
        ))}
    </>
  );
}

function Confirm(props: { t: Translate; text: string; busy?: boolean | undefined; onYes: () => void; onNo: () => void }) {
  return (
    <div className="pv-confirm" role="group">
      <span>{props.text}</span>
      <button type="button" className="pv-btn pv-btn--primary" disabled={props.busy} onClick={props.onYes}>
        {props.t("Yes")}
      </button>
      <button type="button" className="pv-btn" onClick={props.onNo}>
        {props.t("No")}
      </button>
    </div>
  );
}

function StoryStep({ view, t, busy, onAction, zoneNote }: StepProps) {
  const needsPlace = !!view.zone && !view.here;
  return (
    <>
      <ZoneBanner view={view} t={t} note={zoneNote} />
      {view.text && <Markdown className="pv-text" text={view.text} />}
      <button
        type="button"
        className="pv-btn pv-btn--primary pv-btn--wide"
        disabled={busy || needsPlace}
        onClick={() => onAction({ kind: "continue", node: view.node ?? "" })}
      >
        {needsPlace ? t("Continue when you are there") : t("Continue")}
      </button>
    </>
  );
}

function HubStep({ view, t, busy, onAction }: StepProps) {
  const node = view.node ?? "";
  const offer = view.offer ?? [];
  return (
    <>
      <p className="pv-text">{t("Choose where to go next.")}</p>
      <ul className="pv-offer">
        {offer.map((o) => (
          <li key={o.port}>
            <button
              type="button"
              className="pv-btn pv-btn--wide"
              disabled={busy || o.locked}
              onClick={() => onAction({ kind: "choose", node, port: o.port })}
            >
              {o.label || o.port}
              {o.locked && <span className="pv-muted"> · {t("locked")}</span>}
            </button>
          </li>
        ))}
      </ul>
      {view.can_finish && (
        <button
          type="button"
          className="pv-btn pv-btn--primary pv-btn--wide"
          disabled={busy}
          onClick={() => onAction({ kind: "choose", node, port: "done" })}
        >
          {t("Move on")}
        </button>
      )}
      {!view.can_finish && offer.every((o) => o.locked) && (
        <p className="pv-muted">{t("Every way on is taken right now. Wait a moment and try again.")}</p>
      )}
    </>
  );
}

function EndStep({ view, t }: StepProps) {
  const failed = view.outcome === "fail";
  return (
    <div className={"pv-end" + (failed ? " is-fail" : "")}>
      <p className="pv-end__head">{failed ? t("The adventure is over.") : t("You made it!")}</p>
      {view.text && <Markdown className="pv-text" text={view.text} />}
      <dl className="pv-stats">
        <dt>{t("Points")}</dt>
        <dd>{round(view.score)}</dd>
        {view.penalty_score > 0 && (
          <>
            <dt>{t("Hints and penalties")}</dt>
            <dd>−{round(view.penalty_score)}</dd>
          </>
        )}
        <dt>{t("Total")}</dt>
        <dd>
          <strong>{round(view.score - view.penalty_score)}</strong>
        </dd>
      </dl>
    </div>
  );
}

/** What the team is carrying and has read. Hidden when both are empty. */
function Bag({ view, t }: { view: play.View; t: Translate }) {
  const items = view.items ?? [];
  const journal = view.journal ?? [];
  if (items.length === 0 && journal.length === 0) return null;
  return (
    <footer className="pv-bag">
      {items.length > 0 && (
        <details>
          <summary>{t("Items ({{n}})", { n: items.length })}</summary>
          <ul>
            {items.map((it) => (
              <li key={it.slug}>
                {it.icon && <span aria-hidden>{it.icon} </span>}
                {it.name || it.slug}
                {it.qty > 1 && ` ×${it.qty}`}
              </li>
            ))}
          </ul>
        </details>
      )}
      {journal.length > 0 && (
        <details>
          <summary>{t("Journal ({{n}})", { n: journal.length })}</summary>
          {journal.map((j) => (
            <article key={j.slug} className="pv-journal">
              <h3>{j.title || j.slug}</h3>
              {j.body && <Markdown text={j.body} />}
            </article>
          ))}
        </details>
      )}
    </footer>
  );
}

const round = (n: number) => Math.round(n * 10) / 10;
