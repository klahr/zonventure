import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import type { play } from "@zonventure/api";
import { Markdown } from "./Markdown.tsx";
import { brandVars, isHex } from "./brand.ts";
import { formatSeconds, secondsUntil, useEngineClock } from "./clock.ts";
import { english, type Feedback, type Translate } from "./feedback.ts";

/**
 * One step of an adventure, as a player sees it.
 *
 * This is the only renderer of a View. The player app shows the live
 * game with it; the designer's preview and simulator show the draft with
 * it. An author therefore previews the exact screen, wording, colours
 * and motion a team will get — there is no "preview version" to drift.
 *
 * The look is play.html's, which players know: the brand face for
 * titles and moments, the answer bar docked at the bottom in the same
 * place on every step, a "?" for hints that opens a sheet, confetti for
 * a right answer and a shake for a wrong one, each step settling into
 * place piece by piece. All motion stops under prefers-reduced-motion.
 *
 * It is controlled: it holds only what is being typed, which sheet is
 * open and which moment is playing. Everything else comes from the view,
 * and every action goes out through onAction. It never moves on by
 * itself — a wrong optimistic advance shows the next clue to a team that
 * has not earned it.
 *
 * The surface is always light, as play.html's was: outdoor sunlight
 * readability beats night comfort, whatever the device theme.
 */
export interface PlayViewProps {
  view: play.View;
  onAction: (action: play.PlayAction) => void;
  /** An action is in flight; controls are disabled. */
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
  /** The adventure's palette. */
  brand?: play.Brand | null;
}

export function PlayView(props: PlayViewProps) {
  const { view, brand } = props;
  const t = props.t ?? english;
  const now = useEngineClock(view.now, props.live ?? true);
  const burst = useBurst(props.feedback ?? null);
  const [hintsOpen, setHintsOpen] = useState(false);
  const [bagOpen, setBagOpen] = useState(false);
  const style = useMemo(() => brandVars(brand) as CSSProperties, [brand]);

  // A new step closes a sheet left open on the old one.
  useEffect(() => {
    setHintsOpen(false);
    setBagOpen(false);
  }, [view.node]);

  const kind = view.finished || view.type === "end" ? "end" : view.type;
  const step: StepProps = { ...props, t, now, openHints: () => setHintsOpen(true) };

  let body: ReactNode;
  if (kind === "end") body = <EndStep {...step} />;
  else if (kind === "puzzle") body = <PuzzleStep {...step} />;
  else if (kind === "story") body = <StoryStep {...step} />;
  else if (kind === "hub") body = <HubStep {...step} />;
  else body = <p className="pv-muted">{t("Moving on…")}</p>;

  const readyHints = view.next_hint && secondsUntil(view.next_hint.available_at, now) <= 0 ? 1 : 0;

  return (
    <div className={`pv pv--${kind ?? "none"}` + (isHex(brand?.submit_bg) ? " pv--dock-brand" : "")} data-theme="light" style={style}>
      <header className="pv-head">
        <div className="pv-score" aria-label={t("Score")}>
          <span className="pv-score__n">{round(view.score - view.penalty_score)}</span>
          <span className="pv-score__l">{t("pts")}</span>
        </div>
        {kind !== "end" && (view.items?.length || view.journal?.length) ? (
          <button type="button" className="pv-bag-toggle" onClick={() => setBagOpen(true)} aria-label={t("Items and journal")}>
            <span aria-hidden>🎒</span> {(view.items?.length ?? 0) + (view.journal?.length ?? 0)}
          </button>
        ) : null}
        {kind === "puzzle" && (view.hint_count ?? 0) > 0 && (
          <button type="button" className="pv-hint-toggle" onClick={() => setHintsOpen(true)} aria-label={t("Hints")}>
            <span className="pv-hint-toggle__glyph" aria-hidden>?</span>
            {t("Hint")}
            {readyHints > 0 && <span className="pv-hint-toggle__badge">{readyHints}</span>}
          </button>
        )}
      </header>

      {/* Keyed on the node, so each step plays its entrance — held back
          while a celebration covers the screen, then released. */}
      <div key={view.node ?? "none"} className={"pv-step" + (burst?.kind === "celebrate" ? "" : " is-entered")}>
        {body}
      </div>

      {hintsOpen && <HintSheet {...step} onClose={() => setHintsOpen(false)} />}
      {bagOpen && (
        <Sheet title={t("Items and journal")} t={t} onClose={() => setBagOpen(false)}>
          <Bag view={view} t={t} />
        </Sheet>
      )}
      {burst && <BurstOverlay burst={burst} />}
    </div>
  );
}

interface StepProps extends PlayViewProps {
  t: Translate;
  now: number;
  openHints: () => void;
}

// --- moments ----------------------------------------------------------------

type Burst = NonNullable<Feedback["burst"]> & { id: number };

const BURST_MS = { celebrate: 1500, rebuff: 900 } as const;

/** Plays each new feedback's burst once, unless motion is reduced. */
function useBurst(feedback: Feedback | null): Burst | null {
  const [burst, setBurst] = useState<Burst | null>(null);
  const seq = useRef(0);
  useEffect(() => {
    if (!feedback?.burst || prefersReducedMotion()) return;
    const b = { ...feedback.burst, id: ++seq.current };
    setBurst(b);
    const id = setTimeout(() => setBurst((cur) => (cur?.id === b.id ? null : cur)), BURST_MS[b.kind]);
    return () => clearTimeout(id);
  }, [feedback]);
  return burst;
}

function prefersReducedMotion(): boolean {
  return typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
}

const CONFETTI = ["#e24080", "#ad1357", "#ffc93c", "#4cc38a", "#3b82f6", "#ff8a3d"];

function BurstOverlay({ burst }: { burst: Burst }) {
  // Pieces are fixed per burst, so a re-render does not reshuffle them.
  const pieces = useMemo(
    () =>
      burst.kind !== "celebrate"
        ? []
        : Array.from({ length: 36 }, (_, i) => ({
            left: `${Math.random() * 100}%`,
            "--c": CONFETTI[i % CONFETTI.length],
            "--d": `${Math.round(Math.random() * 350)}ms`,
            "--dx": `${Math.round((Math.random() - 0.5) * 220)}px`,
            "--rot": `${Math.round((Math.random() - 0.5) * 1080)}deg`,
          })),
    [burst.id, burst.kind],
  );
  if (burst.kind === "celebrate") {
    return (
      <div className="pv-burst pv-burst--celebrate" aria-hidden>
        {pieces.map((p, i) => (
          <i key={i} className="pv-confetti" style={p as CSSProperties} />
        ))}
        <div className="pv-burst__disc">
          <svg viewBox="0 0 52 52" className="pv-burst__mark">
            <path d="M14 27 l8 8 l16 -18" />
          </svg>
        </div>
        <div className="pv-burst__head">{burst.head}</div>
        {burst.sub && <div className="pv-burst__sub">{burst.sub}</div>}
      </div>
    );
  }
  return (
    <div className="pv-burst pv-burst--rebuff" aria-hidden>
      <div className="pv-burst__disc">
        <svg viewBox="0 0 52 52" className="pv-burst__mark">
          <path d="M17 17 L35 35 M35 17 L17 35" />
        </svg>
      </div>
      <div className="pv-burst__head">{burst.head}</div>
    </div>
  );
}

// --- shared pieces --------------------------------------------------------

function FeedbackLine({ feedback }: { feedback: Feedback | null | undefined }) {
  // Always rendered, so a screen reader announces each new message.
  return (
    <p className={"pv-result" + (feedback ? ` pv-result--${feedback.tone}` : "")} role="status" aria-live="polite">
      {feedback?.text}
    </p>
  );
}

function Title({ view }: { view: play.View }) {
  return view.title ? <h2 className="pv-title">{view.title}</h2> : null;
}

function ZoneCard({ view, t, note }: { view: play.View; t: Translate; note?: ReactNode }) {
  const z = view.zone;
  if (!z) return null;
  const name = z.name || t("the marked place");
  const map = `https://www.openstreetmap.org/?mlat=${z.lat}&mlon=${z.lng}#map=18/${z.lat}/${z.lng}`;
  return (
    <div className={"pv-zone" + (view.here ? " is-here" : "")}>
      <span className="pv-zone__pin" aria-hidden>
        {view.here ? "✓" : "⌖"}
      </span>
      <div className="pv-zone__body">
        <strong>{view.here ? t("You are at {{place}}.", { place: name }) : t("Go to {{place}}.", { place: name })}</strong>
        {!view.here && note && <span className="pv-zone__note">{note}</span>}
      </div>
      <a href={map} target="_blank" rel="noopener noreferrer">
        {t("Map")}
      </a>
    </div>
  );
}

// --- steps ------------------------------------------------------------------

function PuzzleStep(props: StepProps) {
  const { view, t, now, busy, onAction, feedback } = props;
  const node = view.node ?? "";
  const [guess, setGuess] = useState("");
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

  // A right answer moves the team on and remounts this step; a wrong one
  // keeps the text so a typo can be fixed rather than retyped.
  const submit = (e: { preventDefault(): void }) => {
    e.preventDefault();
    const g = guess.trim();
    if (!g || busy) return;
    onAction({ kind: "answer", node, guess: g });
  };

  const input = view.input ?? { kind: "text" as const };
  const placeholder = input.parts
    ? t(input.ordered ? "{{n}} answers, in order, with commas" : "{{n}} answers, with commas", { n: input.parts })
    : t("Your answer");

  return (
    <>
      <Title view={view} />
      <ZoneCard view={view} t={t} note={props.zoneNote} />
      {view.text && <Markdown className="pv-text" text={view.text} />}

      {view.deadline && (
        <p className={"pv-timer" + (timeLeft < 60 ? " is-urgent" : "")}>
          <span aria-hidden>⏱</span> {timeUp ? t("Time is up.") : t("Time left: {{time}}", { time: formatSeconds(timeLeft) })}
        </p>
      )}

      {view.hints && view.hints.length > 0 && (
        <button type="button" className="pv-hints-peek" onClick={props.openHints}>
          {t("{{n}} hints revealed", { n: view.hints.length })} →
        </button>
      )}

      {view.can_skip && <SkipLink {...props} />}

      <div className="pv-dock">
        <FeedbackLine feedback={feedback} />
        {timeUp ? (
          view.has_timeout && !live ? (
            <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={busy} onClick={() => onAction({ kind: "timeout", node })}>
              {t("Continue")}
            </button>
          ) : null
        ) : (
          <form className="pv-answer" onSubmit={submit}>
            <input
              className="pv-input"
              aria-label={t("Your answer")}
              placeholder={needsPlace ? t("Not there yet") : placeholder}
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
              {blockedFor > 0 ? formatSeconds(blockedFor) : t("Answer")}
            </button>
          </form>
        )}
      </div>
    </>
  );
}

function SkipLink({ view, t, busy, onAction }: StepProps) {
  const [confirm, setConfirm] = useState(false);
  const node = view.node ?? "";
  if (!confirm) {
    return (
      <button type="button" className="pv-link" disabled={busy} onClick={() => setConfirm(true)}>
        {t("Skip this puzzle")}
      </button>
    );
  }
  return (
    <div className="pv-confirm" role="group">
      <span>{t("Skip this puzzle? You won't get its points.")}</span>
      <button type="button" className="pv-btn pv-btn--primary" disabled={busy} onClick={() => onAction({ kind: "skip", node })}>
        {t("Yes")}
      </button>
      <button type="button" className="pv-btn" onClick={() => setConfirm(false)}>
        {t("No")}
      </button>
    </div>
  );
}

function HintSheet(props: StepProps & { onClose: () => void }) {
  const { view, t, now, busy, onAction, onClose } = props;
  const [confirm, setConfirm] = useState(false);
  const hint = view.next_hint;
  const hintIn = hint ? secondsUntil(hint.available_at, now) : 0;
  const node = view.node ?? "";

  const reveal = () => {
    setConfirm(false);
    if (hint) onAction({ kind: "hint", node, index: hint.index });
  };

  return (
    <Sheet title={t("Hints")} t={t} onClose={onClose}>
        <div className="pv-sheet__body">
          {view.hints?.map((h, i) => (
            <div key={i} className="pv-hint">
              <div className="pv-hint__n">{t("Hint {{n}}", { n: i + 1 })}</div>
              <Markdown text={h} />
            </div>
          ))}
          {!view.hints?.length && !hint && <p className="pv-muted">{t("No hints for this one.")}</p>}
        </div>
        {hint &&
          (hintIn > 0 ? (
            <p className="pv-sheet__locked">{t("Next hint in {{time}}", { time: formatSeconds(hintIn) })}</p>
          ) : confirm ? (
            <div className="pv-confirm">
              <span>{t("This hint costs {{n}} points.", { n: hint.cost ?? 0 })}</span>
              <button type="button" className="pv-btn pv-btn--primary" disabled={busy} onClick={reveal}>
                {t("Yes")}
              </button>
              <button type="button" className="pv-btn" onClick={() => setConfirm(false)}>
                {t("No")}
              </button>
            </div>
          ) : (
            <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={busy} onClick={() => (hint.cost ? setConfirm(true) : reveal())}>
              {hint.cost ? t("Get a hint (−{{n}} points)", { n: hint.cost }) : t("Get a hint")}
            </button>
          ))}
    </Sheet>
  );
}

/** A bottom sheet over the step. Escape or a tap outside closes it. */
function Sheet({ title, t, onClose, children }: { title: string; t: Translate; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="pv-sheet-back" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="pv-sheet" role="dialog" aria-modal="true" aria-label={title}>
        <div className="pv-sheet__head">
          <h3>{title}</h3>
          <button type="button" className="pv-x" onClick={onClose} aria-label={t("Close")}>
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

function StoryStep({ view, t, busy, onAction, zoneNote, feedback }: StepProps) {
  const needsPlace = !!view.zone && !view.here;
  return (
    <>
      <Title view={view} />
      <ZoneCard view={view} t={t} note={zoneNote} />
      {view.text && <Markdown className="pv-text" text={view.text} />}
      <div className="pv-dock">
        <FeedbackLine feedback={feedback} />
        <button
          type="button"
          className="pv-btn pv-btn--primary pv-btn--wide"
          disabled={busy || needsPlace}
          onClick={() => onAction({ kind: "continue", node: view.node ?? "" })}
        >
          {needsPlace ? t("Continue when you are there") : t("Continue")}
        </button>
      </div>
    </>
  );
}

function HubStep({ view, t, busy, onAction, feedback }: StepProps) {
  const node = view.node ?? "";
  const offer = view.offer ?? [];
  return (
    <>
      <Title view={view} />
      <p className="pv-text">{t("Choose where to go next.")}</p>
      <ul className="pv-offer">
        {offer.map((o, i) => (
          <li key={o.port}>
            <button
              type="button"
              className="pv-tile"
              style={{ "--i": i } as CSSProperties}
              disabled={busy || o.locked}
              onClick={() => onAction({ kind: "choose", node, port: o.port })}
            >
              <span className="pv-tile__label">{o.label || o.port}</span>
              <span className="pv-tile__go" aria-hidden>
                {o.locked ? "🔒" : "→"}
              </span>
              {o.locked && <span className="pv-visually-hidden">{t("locked")}</span>}
            </button>
          </li>
        ))}
      </ul>
      {!view.can_finish && offer.every((o) => o.locked) && (
        <p className="pv-muted">{t("Every way on is taken right now. Wait a moment and try again.")}</p>
      )}
      <div className="pv-dock">
        <FeedbackLine feedback={feedback} />
        {view.can_finish && (
          <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={busy} onClick={() => onAction({ kind: "choose", node, port: "done" })}>
            {t("Move on")}
          </button>
        )}
      </div>
    </>
  );
}

function EndStep({ view, t }: StepProps) {
  const failed = view.outcome === "fail";
  return (
    <div className={"pv-summary" + (failed ? " is-fail" : "")}>
      <div className="pv-summary__badge" aria-hidden>
        {failed ? "⚑" : "★"}
      </div>
      <h2 className="pv-summary__head">{failed ? t("The adventure is over.") : t("You made it!")}</h2>
      {view.text && <Markdown className="pv-text" text={view.text} />}
      <div className="pv-stats">
        <div className="pv-stat">
          <div className="pv-stat__v">{round(view.score)}</div>
          <div className="pv-stat__l">{t("Points")}</div>
        </div>
        <div className="pv-stat">
          <div className="pv-stat__v">{view.penalty_score > 0 ? `−${round(view.penalty_score)}` : "0"}</div>
          <div className="pv-stat__l">{t("Hints and penalties")}</div>
        </div>
        <div className="pv-stat pv-stat--total">
          <div className="pv-stat__v">{round(view.score - view.penalty_score)}</div>
          <div className="pv-stat__l">{t("Total")}</div>
        </div>
      </div>
      <Bag view={view} t={t} />
    </div>
  );
}

/** What the team carries and has read. Nothing when both are empty. */
function Bag({ view, t }: { view: play.View; t: Translate }) {
  const items = view.items ?? [];
  const journal = view.journal ?? [];
  return (
    <>
      {items.length > 0 && (
        <section className="pv-bag">
          <h3>{t("Items ({{n}})", { n: items.length })}</h3>
          <ul>
            {items.map((it) => (
              <li key={it.slug}>
                {it.icon && <span aria-hidden>{it.icon} </span>}
                {it.name || it.slug}
                {it.qty > 1 && ` ×${it.qty}`}
              </li>
            ))}
          </ul>
        </section>
      )}
      {journal.length > 0 && (
        <section className="pv-bag">
          <h3>{t("Journal ({{n}})", { n: journal.length })}</h3>
          {journal.map((j) => (
            <article key={j.slug} className="pv-journal">
              <h4>{j.title || j.slug}</h4>
              {j.body && <Markdown text={j.body} />}
            </article>
          ))}
        </section>
      )}
    </>
  );
}

/**
 * The event-start moment: the adventure's title over a bar that fills,
 * then gone. Shown once, when the game begins; skipped entirely under
 * reduced motion, where a frozen ceremony would only be in the way.
 */
export function StartCeremony({ title, brand, onDone }: { title: string; brand?: play.Brand | null; onDone: () => void }) {
  const style = useMemo(() => brandVars(brand) as CSSProperties, [brand]);
  // Once per mount, whatever the parent re-renders: a fresh onDone each
  // render must not keep pushing the end back.
  const done = useRef(onDone);
  done.current = onDone;
  useEffect(() => {
    const id = setTimeout(() => done.current(), prefersReducedMotion() ? 0 : 1300);
    return () => clearTimeout(id);
  }, []);
  return (
    <div className="pv-start" data-theme="light" style={style} aria-hidden>
      <h1>{title}</h1>
      <div className="pv-start__bar">
        <i />
      </div>
    </div>
  );
}

const round = (n: number) => Math.round(n * 10) / 10;
