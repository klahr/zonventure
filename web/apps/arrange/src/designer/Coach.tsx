import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowRight, Check, ChevronDown, CircleCheck, Copy, GraduationCap, ListChecks, Pause, RotateCcw, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { useDialogs } from "../ui/dialogs.tsx";
import { Button, DzPortal, ICON } from "./dz/ui.tsx";
import { coachAt, DONE, type CoachAct, type CoachCtx, type CoachProgress, type CoachRun, type Finder, type Tutorial, type TutorialId } from "./coach.ts";

type Rect = { x: number; y: number; w: number; h: number };
type Side = "left" | "right" | "above" | "below";

const PAD = 6;
const ARROW = 56;
const GAP = 16;
const CARD_W = 340;

function visible(el: Element) {
  const r = el.getBoundingClientRect();
  return r.width > 0 && r.height > 0;
}

/** The first finder's first visible match. */
export function find(fs: Finder[]): Element | null {
  for (const f of fs) {
    for (const el of document.querySelectorAll(f.sel)) {
      if (!visible(el)) continue;
      if (f.text !== undefined) {
        const label = el.querySelector(".field__label, .context-menu-label")?.textContent ?? el.textContent;
        if (label?.trim() !== f.text) continue;
      }
      const at = f.then ? el.querySelector(f.then) : f.up ? (el.closest(f.up) ?? el) : el;
      if (at && visible(at)) return at;
    }
  }
  return null;
}

const rectOf = (el: Element | null): Rect | null => {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.left - PAD, y: r.top - PAD, w: r.width + PAD * 2, h: r.height + PAD * 2 };
};
const same = (a: Rect | null, b: Rect | null) =>
  a === b || (!!a && !!b && Math.round(a.x) === Math.round(b.x) && Math.round(a.y) === Math.round(b.y) && Math.round(a.w) === Math.round(b.w) && Math.round(a.h) === Math.round(b.h));

/** Room beside a rect, on each side. */
const room = (r: Rect): Record<Side, number> => ({
  left: r.x,
  right: innerWidth - (r.x + r.w),
  above: r.y,
  below: innerHeight - (r.y + r.h),
});

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

/**
 * Where the card goes: beside the arrow, the arrow beside the target, so
 * the eye runs card → arrow → target. A target too big to sit beside
 * gets the card in the corner farthest from it.
 */
function layout(point: Rect, target: Rect, cardH: number): { side: Side | null; card: { left: number; top: number } } {
  const cx = point.x + point.w / 2;
  const cy = point.y + point.h / 2;
  const maxL = innerWidth - CARD_W - GAP;
  const maxT = innerHeight - cardH - GAP;
  const at: Record<Side, { left: number; top: number }> = {
    right: { left: point.x + point.w + ARROW + GAP, top: clamp(cy - cardH / 2, GAP, maxT) },
    left: { left: point.x - ARROW - GAP - CARD_W, top: clamp(cy - cardH / 2, GAP, maxT) },
    below: { left: clamp(cx - CARD_W / 2, GAP, maxL), top: point.y + point.h + ARROW + GAP },
    above: { left: clamp(cx - CARD_W / 2, GAP, maxL), top: point.y - ARROW - GAP - cardH },
  };
  const fits = (c: { left: number; top: number }) =>
    c.left >= GAP && c.top >= GAP && c.left <= maxL && c.top <= maxT && !overlaps({ x: c.left, y: c.top, w: CARD_W, h: cardH }, target);
  const side = (["right", "left", "below", "above"] as Side[]).find((x) => fits(at[x])) ?? null;
  if (side) return { side, card: at[side] };
  // Too big to sit beside: a corner clear of the spotlight, else the farthest one.
  const corners = [
    { left: maxL, top: maxT },
    { left: GAP, top: maxT },
    { left: maxL, top: GAP },
    { left: GAP, top: GAP },
  ];
  const far = { left: cx < innerWidth / 2 ? maxL : GAP, top: cy < innerHeight / 2 ? maxT : GAP };
  return { side: null, card: corners.find(fits) ?? far };
}

const overlaps = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** The arrow on the given side of the target, pointing at it. */
function arrowAt(point: Rect, side: Side | null, card: { left: number; top: number }, cardH: number) {
  let s = side;
  if (!s) {
    // The card is in a corner: point from the side facing it that has room.
    const space = room(point);
    const toward: Side[] = [card.left > point.x ? "right" : "left", card.top > point.y ? "below" : "above"];
    s = toward.find((x) => space[x] >= ARROW + GAP) ?? (["left", "above", "right", "below"] as Side[]).find((x) => space[x] >= ARROW + GAP) ?? "left";
  }
  const cx = point.x + point.w / 2;
  const cy = point.y + point.h / 2;
  // The arrow's middle sits between card and target where they overlap,
  // otherwise at the target's middle.
  const midY = side ? clamp(cy, card.top + 24, card.top + cardH - 24) : cy;
  const midX = side ? clamp(cx, card.left + 24, card.left + CARD_W - 24) : cx;
  const y = clamp(midY, point.y + 8, point.y + point.h - 8);
  const x = clamp(midX, point.x + 8, point.x + point.w - 8);
  switch (s) {
    case "right":
      return { side: s, left: point.x + point.w + 4, top: y - ARROW / 2, rotate: 180 };
    case "left":
      return { side: s, left: point.x - ARROW - 4, top: y - ARROW / 2, rotate: 0 };
    case "below":
      return { side: s, left: x - ARROW / 2, top: point.y + point.h + 4, rotate: 270 };
    default:
      return { side: s, left: x - ARROW / 2, top: point.y - ARROW - 4, rotate: 90 };
  }
}

const LOCKED = ["pointerdown", "mousedown", "mouseup", "click", "dblclick", "contextmenu", "touchstart", "touchend"] as const;

/**
 * The tutorial on a desktop: a card that says what to do next, a
 * spotlight on where, and a pulsing arrow pointing at it. Outside the
 * spotlight nothing takes a click, so nothing is touched by mistake;
 * Pause hands the whole designer back.
 */
export function Coach({
  tutorial,
  next: upNext,
  run,
  ctx,
  onAct,
  onRun,
  onDone,
  onPause,
  onList,
  onStop,
  onRestart,
  onStart,
}: {
  tutorial: Tutorial;
  /** The lesson after this one not yet done, offered when this one ends. */
  next: Tutorial | undefined;
  run: CoachRun;
  ctx: Omit<CoachCtx, "palette">;
  onAct: (act: CoachAct, text?: string) => void;
  onRun: (r: CoachRun) => void;
  /** The lesson's last step is behind it. */
  onDone: () => void;
  onPause: () => void;
  /** Back to the list of lessons. */
  onList: () => void;
  onStop: () => void;
  /** The lesson from its first step, counting from what the adventure has now. */
  onRestart: () => void;
  onStart: (id: TutorialId) => void;
}) {
  const t = useT();
  const dialogs = useDialogs();
  const steps = tutorial.steps;
  const [palette, setPalette] = useState(false);
  const full: CoachCtx = { ...ctx, palette };
  const held = !!run.held;
  const i = coachAt(steps, run.at, full, held);
  const finished = i >= steps.length;
  const step = steps[Math.min(i, steps.length - 1)]!;

  // Remember the step reached, so a reload carries on from it.
  const at = finished ? DONE : step.id;
  useEffect(() => {
    if (at !== run.at) onRun({ ...run, at });
  }, [at, run, onRun]);
  useEffect(() => {
    if (finished) onDone();
  }, [finished, onDone]);
  // Gone back to a step not done: doing it moves on again by itself.
  const stepDone = !finished && !!step.done?.(full);
  useEffect(() => {
    if (held && !finished && !stepDone) onRun({ ...run, held: false });
  }, [held, finished, stepDone, run, onRun]);

  const [target, setTarget] = useState<Rect | null>(null);
  const [point, setPoint] = useState<Rect | null>(null);
  const targetEl = useRef<Element | null>(null);
  const live = useRef({ step, full, finished });
  live.current = { step, full, finished };

  // Follow the target as it moves, opens and closes: the canvas pans,
  // dialogs animate, the inspector swaps pages.
  useEffect(() => {
    let raf = 0;
    let scrolled: Element | null = null;
    const tick = () => {
      const { step: s, full: c, finished: over } = live.current;
      setPalette(!!document.querySelector(".qp"));
      const el = !over && s.target ? find(s.target(c)) : null;
      targetEl.current = el;
      if (el && el !== scrolled) {
        scrolled = el;
        const r = el.getBoundingClientRect();
        if (r.top < 0 || r.bottom > innerHeight) el.scrollIntoView({ block: "center", behavior: "smooth" });
      }
      const p = (!over && s.point && find(s.point(c))) || el;
      const tr = rectOf(el);
      const pr = rectOf(p);
      setTarget((old) => (same(old, tr) ? old : tr));
      setPoint((old) => (same(old, pr) ? old : pr));
      raf = requestAnimationFrame(tick);
    };
    tick();
    return () => cancelAnimationFrame(raf);
  }, [at]);

  // Next from a step gone back to stays held, so done steps are walked one by one.
  const next = () => onRun({ ...run, at: steps[i + 1]?.id ?? DONE, held });
  const back = () => {
    const prev = steps[i - 1];
    if (prev) onRun({ ...run, at: prev.id, held: true });
  };

  // A question in the app's own dialog: the spotlight and the lock step
  // aside until it is answered.
  const [asking, setAsking] = useState(false);
  const asked = useRef(false);
  asked.current = asking;
  const ask = async (o: Parameters<typeof dialogs.confirm>[0]) => {
    setAsking(true);
    try {
      return await dialogs.confirm(o);
    } finally {
      setAsking(false);
    }
  };

  // The lock: clicks land only in the spotlight, the step's allowed
  // places and the card. A step whose target can't be found locks
  // nothing, so the author is never stuck.
  const card = useRef<HTMLDivElement>(null);
  const advance = useRef(next);
  advance.current = next;
  useEffect(() => {
    const block = (e: Event) => {
      if (asked.current) return;
      const { step: s, finished: over } = live.current;
      const at = e.target as Element | null;
      if (!at || !(at instanceof Element)) return;
      if (card.current?.contains(at)) return;
      // The markdown toolbar's menus are portaled out of the field.
      if (at.closest(".md-menu")) return;
      const el = targetEl.current;
      if (el?.contains(at)) {
        if (s.onClick && e.type === "click") setTimeout(() => advance.current(), 0);
        return;
      }
      if (!over && s.allow?.some((sel) => at.closest(sel))) return;
      if (!over && s.target && !el) return;
      e.preventDefault();
      e.stopPropagation();
    };
    for (const ev of LOCKED) window.addEventListener(ev, block, { capture: true });
    return () => {
      for (const ev of LOCKED) window.removeEventListener(ev, block, { capture: true });
    };
  }, []);

  const [cardH, setCardH] = useState(240);
  useLayoutEffect(() => {
    const h = card.current?.offsetHeight;
    if (h && Math.abs(h - cardH) > 2) setCardH(h);
  });

  const [copied, setCopied] = useState(false);
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };
  const stop = async () => {
    if (
      finished ||
      (await ask({
        title: t.t("Stop this tutorial?"),
        message: t.t("Your adventure stays as it is. You can start it again from the ⋮ menu."),
        confirmLabel: t.t("Stop"),
        cancelLabel: t.t("Keep going"),
      }))
    )
      onStop();
  };
  const restart = async () => {
    if (
      await ask({
        title: t.t("Start this tutorial over?"),
        message: t.t("It begins again from the first step. What you made so far stays in the adventure."),
        confirmLabel: t.t("Start over"),
        cancelLabel: t.t("Keep going"),
      })
    )
      onRestart();
  };

  const searching = !finished && !!step.target && !target;
  const lay = point ? layout(point, target ?? point, cardH) : null;
  const arrow = point && lay ? arrowAt(point, lay.side, lay.card, cardH) : null;
  const pos = lay ? lay.card : { left: (innerWidth - CARD_W) / 2, top: Math.max(GAP, (innerHeight - cardH) / 2) };
  const needsNext = held || (!step.done && !step.action && !step.onClick);
  const ready = held || !step.ready || step.ready(full);

  if (asking) return null;

  return (
    <DzPortal>
      {target ? (
        <div className="coach-spot" style={{ left: target.x, top: target.y, width: target.w, height: target.h }} aria-hidden />
      ) : (
        !searching && <div className="coach-dim" aria-hidden />
      )}
      {arrow && (
        <div className={"coach-arrow coach-arrow--" + arrow.side} style={{ left: arrow.left, top: arrow.top }} aria-hidden>
          <ArrowRight size={ARROW} strokeWidth={2.5} style={{ transform: `rotate(${arrow.rotate}deg)` }} />
        </div>
      )}
      <div ref={card} className="coach" role="dialog" aria-live="polite" aria-label={tutorial.title} style={{ left: pos.left, top: pos.top, width: CARD_W }}>
        <header className="coach__head">
          {finished ? <CircleCheck size={ICON.md} aria-hidden /> : <GraduationCap size={ICON.md} aria-hidden />}
          <strong>{finished ? t.t("{{title}}: done", { title: tutorial.title }) : step.title}</strong>
          <Button variant="ghost" size="icon" onClick={onList} aria-label={t.t("All tutorials")} title={t.t("All tutorials")}>
            <ListChecks size={ICON.md} />
          </Button>
          <Button variant="ghost" size="icon" onClick={() => void restart()} aria-label={t.t("Start over")} title={t.t("Start over")}>
            <RotateCcw size={ICON.md} />
          </Button>
          {!finished && (
            <Button variant="ghost" size="icon" onClick={onPause} aria-label={t.t("Pause the tutorial")} title={t.t("Pause the tutorial")}>
              <Pause size={ICON.md} />
            </Button>
          )}
          <Button variant="ghost" size="icon" onClick={() => void stop()} aria-label={t.t("Stop the tutorial")} title={t.t("Stop the tutorial")}>
            <X size={ICON.md} />
          </Button>
        </header>
        {/* One dot per step: steps passed by because they were done already show ticked, so the count can jump. */}
        <ol className="coach__dots" role="progressbar" aria-valuemin={0} aria-valuemax={steps.length} aria-valuenow={i}>
          {steps.map((s, k) => (
            <li key={s.id} className={k < i ? "is-done" : k === i ? "is-at" : undefined}>
              {k < i && <Check size={8} strokeWidth={4} aria-hidden />}
            </li>
          ))}
        </ol>
        {finished ? (
          <p className="coach__body">
            {upNext ? t.t("Next up: {{title}}. {{blurb}}", { title: upNext.title, blurb: upNext.blurb }) : t.t("That was the last one: you know your way around the designer now.")}
          </p>
        ) : (
          <p className="coach__body">{step.body}</p>
        )}
        {searching && <p className="coach__lost">{t.t("Can't see it? Everything is unlocked until it shows up again.")}</p>}
        {!finished && step.example && (
          <div className="coach__example">
            <pre>{step.example}</pre>
            <div className="coach__row">
              <Button variant="ghost" size="sm" onClick={() => copy(step.example!)}>
                {copied ? <Check size={ICON.xs} aria-hidden /> : <Copy size={ICON.xs} aria-hidden />}
                {copied ? t.t("Copied") : t.t("Copy")}
              </Button>
              {step.fill && (
                <Button variant="ghost" size="sm" onClick={() => onAct(step.fill!, step.example)}>
                  {t.t("Write it for me")}
                </Button>
              )}
            </div>
          </div>
        )}
        <footer className="coach__foot">
          <span className="coach__count">
            {tutorial.title} · {Math.min(i + 1, steps.length)}/{steps.length}
          </span>
          {i > 0 && (
            <Button variant="ghost" size="sm" onClick={back}>
              {t.t("Back")}
            </Button>
          )}
          {finished ? (
            <>
              <Button variant="ghost" size="sm" onClick={onList}>
                {t.t("All tutorials")}
              </Button>
              {upNext ? (
                <Button variant="primary" size="sm" onClick={() => onStart(upNext.id)}>
                  {t.t("Start")}
                </Button>
              ) : (
                <Button variant="primary" size="sm" onClick={onStop}>
                  {t.t("Close")}
                </Button>
              )}
            </>
          ) : (
            <>
              {!held && (
                <Button variant="ghost" size="sm" onClick={next}>
                {t.t("Skip")}
              </Button>
              )}
              {step.action && !held && (
                <Button variant="primary" size="sm" onClick={() => onAct(step.action!.act)}>
                  {step.action.label}
                </Button>
              )}
              {needsNext && (
                <Button variant="primary" size="sm" disabled={!ready} onClick={next}>
                  {t.t("Next")}
                </Button>
              )}
            </>
          )}
        </footer>
      </div>
    </DzPortal>
  );
}

/**
 * The list of lessons, docked over the canvas: each with a tick once done,
 * and a button to start, carry on, or do it again. A lesson paused is
 * picked up where it stopped.
 */
export function TutorialList({
  tutorials,
  progress,
  onStart,
  onResume,
  onMin,
  onClose,
}: {
  tutorials: Tutorial[];
  progress: CoachProgress;
  onStart: (id: TutorialId) => void;
  onResume: () => void;
  onMin: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const count = tutorials.filter((x) => progress.done.includes(x.id)).length;
  const run = progress.run;
  const going = (x: Tutorial) => run?.id === x.id && run.at !== DONE;
  // One button stands out: the lesson under way, else the first not done.
  const lead = tutorials.find(going) ?? tutorials.find((x) => !progress.done.includes(x.id));
  return (
    <aside className="tuts" aria-label={t.t("Tutorials")}>
      <header className="tuts__head">
        <GraduationCap size={ICON.md} aria-hidden />
        <strong>{t.t("Tutorials")}</strong>
        <span className="tuts__count">{t.t("{{done}} of {{n}} done", { done: count, n: tutorials.length })}</span>
        <Button variant="ghost" size="icon" onClick={onMin} aria-label={t.t("Hide the tutorials")} title={t.t("Hide the tutorials")}>
          <ChevronDown size={ICON.md} />
        </Button>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label={t.t("Close the tutorials")} title={t.t("Close the tutorials")}>
          <X size={ICON.md} />
        </Button>
      </header>
      <p className="tuts__lead">{t.t("Short guided lessons, a few minutes each. Do them in any order, and pick up later where you left off.")}</p>
      <ol className="tuts__list">
        {tutorials.map((x, n) => {
          const done = progress.done.includes(x.id);
          const on = going(x);
          return (
            <li key={x.id} className={"tuts__item" + (done ? " is-done" : "") + (on ? " is-going" : "")}>
              <span className="tuts__n" role="img" aria-label={done ? t.t("Done") : t.t("Not done yet")}>
                {done ? <Check size={ICON.xs} aria-hidden /> : n + 1}
              </span>
              <span className="tuts__text">
                <span className="tuts__title">{x.title}</span>
                <span className="tuts__blurb">{x.blurb}</span>
                <span className="tuts__meta">{on ? t.t("In progress") : t.t("{{n}} steps", { n: x.steps.length })}</span>
              </span>
              <Button variant={x === lead ? "primary" : "ghost"} size="sm" onClick={() => (on ? onResume() : onStart(x.id))}>
                {on ? t.t("Resume") : done ? t.t("Again") : t.t("Start")}
              </Button>
            </li>
          );
        })}
      </ol>
    </aside>
  );
}

/** The list put away: a pill that brings it back, or the lesson paused. */
export function TutorialPill({ label, onClick }: { label: string; onClick: () => void }) {
  // Clear of the app's sidebar, whose account block sits in the same
  // corner, and of the canvas's zoom buttons beside it.
  const [left, setLeft] = useState<number>();
  useLayoutEffect(() => {
    const bar = document.querySelector(".shell__sidebar");
    if (!bar) return;
    const place = () => {
      const right = Math.max(0, ...[bar, document.querySelector(".react-flow__controls")].map((el) => {
        const r = el?.getBoundingClientRect();
        return r && r.width > 0 && r.right > 0 ? r.right : 0;
      }));
      setLeft(right > 0 ? right + 16 : undefined);
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(bar);
    window.addEventListener("resize", place);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", place);
    };
  }, []);
  return (
    <DzPortal>
      <button type="button" className="coach-pill" style={left === undefined ? undefined : { left }} onClick={onClick}>
        <GraduationCap size={ICON.sm} aria-hidden />
        <span>{label}</span>
      </button>
    </DzPortal>
  );
}
