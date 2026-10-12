import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { coverOf } from "@zonventure/play-ui";
import {
  CardEditor,
  Icon,
  MAX_CARDS,
  PhonePreview,
  SaveNote,
  ThemePicker,
  blank,
  move,
  numbers,
  problems,
  themeById,
  themeStyle,
  type Card,
  type SaveState,
  type TrailTheme,
} from "@zonventure/trail";
import { useT } from "./i18n.tsx";

/**
 * The guide's frame: fullscreen, a slim bar (back, save state, close),
 * one screen's content, and its buttons at the foot within thumb reach.
 */
export function GuideShell({
  style,
  back,
  close,
  status,
  progress,
  children,
  foot,
  hero,
}: {
  style?: CSSProperties;
  back?: (() => void) | undefined;
  close?: (() => void) | undefined;
  status?: ReactNode;
  /** 0–1: how far through the cards. */
  progress?: number | undefined;
  children: ReactNode;
  foot?: ReactNode;
  /** Full width under the bar, above the screen's content. */
  hero?: ReactNode;
}) {
  const t = useT().t;
  return (
    <div className="trail trail-guide" style={style}>
      <header className="trail-guide__bar">
        {back ? (
          <button type="button" className="trail-guide__back" onClick={back}>
            <Icon name="left" size={18} />
            {t("Back")}
          </button>
        ) : (
          <span />
        )}
        <span className="trail-guide__status">{status}</span>
        {close ? (
          <button type="button" className="trail-icon-btn trail-guide__close" aria-label={t("Close")} title={t("Close")} onClick={close}>
            <Icon name="close" size={20} />
          </button>
        ) : (
          <span />
        )}
        {progress !== undefined && (
          <span className="trail-guide__progress" aria-hidden="true">
            <span style={{ width: `${Math.round(progress * 100)}%` }} />
          </span>
        )}
      </header>
      <div className={"trail-main trail-guide__main" + (hero ? " has-hero" : "")}>
        {hero}
        <div className="trail-guide__body">{children}</div>
      </div>
      {foot && (
        <footer className="trail-guide__foot">
          <div>{foot}</div>
        </footer>
      )}
    </div>
  );
}

/** The theme's cover across the top, fading into the page, named on a pill; tap it to change. */
export function ThemeHero({ theme, onChange }: { theme: TrailTheme; onChange?: (() => void) | undefined }) {
  const t = useT().t;
  const cover = coverOf(theme.id);
  return (
    <div className="trail-guide-hero" style={cover ? { backgroundImage: `url(${cover.wide})` } : undefined}>
      {onChange && (
        <button type="button" className="trail-guide-hero__theme" onClick={onChange}>
          <Icon name="palette" size={16} />
          <strong>{t(theme.label)}</strong>
          <span>{t("Change")}</span>
        </button>
      )}
    </div>
  );
}

type Screen = { at: "name" } | { at: "card"; id: string } | { at: "overview" } | { at: "ready" };

export interface GuideProps {
  title: string;
  cards: Card[];
  state: SaveState;
  readOnly: boolean;
  onChange: (next: { title?: string; cards?: Card[] }) => void;
  savedText?: string | undefined;
  theme: string;
  onTheme?: ((t: TrailTheme) => void) | undefined;
  /** A new one: the guide starts at its name. */
  fresh: boolean;
  /** Its cards are a template's examples, marked so until changed. */
  example: boolean;
  /** Paid, or from the host page: the last screen is Overview, with Done. */
  finishOnOverview: boolean;
  /** The last screen's form (teams, email) and the button that sends it. */
  ready?: { body: ReactNode; action: ReactNode } | undefined;
  /** Notes about the trail (a game running, reprint, ended), shown on Overview. */
  notes?: ReactNode;
  /** A passing message (copied, an error), shown on every screen. */
  note?: string | null;
  close: () => void;
  /** Done, on Overview when finishOnOverview. */
  finish: () => void;
  busy: boolean;
  /** The bar's slot for Reload after a conflict. */
  barAction?: ReactNode;
}

/**
 * The public creator as a guide, one thing per screen: the name, then
 * each card, then all the cards (Overview), then Ready. A new party
 * adventure walks it from the name; one opened again starts on Overview.
 * The screen is mirrored in history.state, so the phone's back button
 * steps back through it.
 */
export function Guide(props: GuideProps) {
  const { title, cards, readOnly, onChange } = props;
  const i18n = useT();
  const t = i18n.t;
  const [screen, setScreen] = useState<Screen>(() => (props.fresh && !readOnly ? { at: "name" } : { at: "overview" }));
  const [depth, setDepth] = useState(0);
  // Cards added on this visit and not yet left: no "still needs" until then.
  const [added, setAdded] = useState<Set<string>>(() => new Set());
  // The template's example cards not yet changed.
  const [examples, setExamples] = useState<Set<string>>(() => new Set(props.example ? cards.map((c) => c.id) : []));
  const [looks, setLooks] = useState(false);
  const [peek, setPeek] = useState(false);
  const [undo, setUndo] = useState<{ cards: Card[]; label: string } | null>(null);
  const top = useRef<HTMLDivElement>(null);
  const theme = themeById(props.theme);
  const nums = numbers(cards);
  const probs = problems(cards);
  const flagged = new Set(probs.map((p) => p.card));

  useEffect(() => {
    history.replaceState({ guide: screen, depth: 0 }, "");
    const pop = (e: PopStateEvent) => {
      const st = e.state as { guide?: Screen; depth?: number } | null;
      if (!st?.guide) return;
      setScreen(st.guide);
      setDepth(st.depth ?? 0);
    };
    addEventListener("popstate", pop);
    return () => removeEventListener("popstate", pop);
    // The first screen only; later ones are pushed by go.
  }, []);
  // A card gone from under its screen (deleted, or undone away): Overview has the rest.
  const lost = screen.at === "card" && !cards.some((c) => c.id === screen.id);
  useEffect(() => {
    if (lost) setScreen({ at: "overview" });
  }, [lost]);
  useEffect(() => {
    if (!undo) return;
    const id = setTimeout(() => setUndo(null), 6000);
    return () => clearTimeout(id);
  }, [undo]);

  const go = (next: Screen) => {
    // Leaving a card: from now on it says what it still needs.
    if (screen.at === "card") {
      const id = screen.id;
      setAdded((s) => {
        const n = new Set(s);
        n.delete(id);
        return n;
      });
    }
    history.pushState({ guide: next, depth: depth + 1 }, "");
    setDepth(depth + 1);
    setScreen(next);
    setLooks(false);
    top.current?.closest(".trail-guide")?.scrollTo(0, 0);
  };
  const back = () => (depth > 0 ? history.back() : go({ at: "overview" }));

  const add = (at: number) => {
    if (cards.length >= MAX_CARDS) return;
    const c = blank(cards[at - 1]?.kind ?? "quiz");
    const next = [...cards];
    next.splice(at, 0, c);
    onChange({ cards: next });
    go({ at: "card", id: c.id });
    setAdded((s) => new Set(s).add(c.id));
  };
  const update = (c: Card) => {
    onChange({ cards: cards.map((x) => (x.id === c.id ? c : x)) });
    if (examples.has(c.id))
      setExamples((s) => {
        const n = new Set(s);
        n.delete(c.id);
        return n;
      });
  };
  const reorder = (from: number, to: number) => {
    const next = move(cards, from, to);
    if (next === cards) return;
    setUndo({ cards, label: t("Card moved.") });
    onChange({ cards: next });
  };
  const remove = (id: string) => {
    const i = cards.findIndex((c) => c.id === id);
    const next = cards.filter((c) => c.id !== id);
    setUndo({ cards, label: t("Card deleted.") });
    onChange({ cards: next });
    const stay = next[Math.min(i, next.length - 1)];
    go(stay ? { at: "card", id: stay.id } : { at: "overview" });
  };

  const status = (
    <>
      <SaveNote t={t} state={props.state} savedText={props.savedText} />
      {props.barAction}
    </>
  );
  const shell = (body: ReactNode, foot: ReactNode, opts: { progress?: number; hero?: boolean } = {}) => (
    <GuideShell
      style={themeStyle(theme?.look)}
      close={props.close}
      status={status}
      progress={opts.progress}
      foot={foot}
      hero={opts.hero && theme ? <ThemeHero theme={theme} /> : undefined}
    >
      <div ref={top} />
      {props.note && (
        <p className="party-note" role="status">
          {props.note}
        </p>
      )}
      {body}
      {undo && (
        <div className="trail-undo" role="status">
          <span>{undo.label}</span>
          <button
            type="button"
            onClick={() => {
              onChange({ cards: undo.cards });
              setUndo(null);
            }}
          >
            {t("Undo")}
          </button>
        </div>
      )}
    </GuideShell>
  );
  const backBtn = (onClick: () => void) => (
    <button type="button" className="trail-btn trail-guide__icon" aria-label={t("Back")} title={t("Back")} onClick={onClick}>
      <Icon name="left" size={20} />
    </button>
  );
  const nextBtn = (label: string, onClick: () => void, disabled?: boolean) => (
    <button type="button" className="trail-btn trail-btn--primary trail-guide__next" disabled={disabled} onClick={onClick}>
      {label}
      <Icon name="right" size={18} />
    </button>
  );

  if (screen.at === "name") {
    const first = cards[0];
    return shell(
      <>
        <h1 className="trail-guide__title">{t("What's it called?")}</h1>
        <p className="trail-guide__lead">{t("The teams see the name on their phones.")}</p>
        <div className="trail-guide__namebox">
          <input
            className="trail-guide__name"
            value={title}
            maxLength={120}
            autoFocus
            aria-label={t("Name of the party adventure")}
            placeholder={t("Name of the party adventure")}
            // A suggestion: one tap selects it, so typing replaces it.
            onFocus={(e) => e.currentTarget.select()}
            onChange={(e) => onChange({ title: e.target.value })}
          />
          <Icon name="pencil" size={18} />
        </div>
      </>,
      nextBtn(t("Next"), () => (first ? go({ at: "card", id: first.id }) : add(0)), !title.trim()),
      { hero: true },
    );
  }

  if (screen.at === "card") {
    const i = cards.findIndex((c) => c.id === screen.id);
    const card = cards[i];
    if (!card) return null;
    const last = i === cards.length - 1;
    const canAdd = last && !readOnly && cards.length < MAX_CARDS;
    return shell(
      <>
        {examples.has(card.id) && !readOnly && (
          <p className="trail-guide__example">
            <Icon name="pencil" size={16} />
            {t("Example — tap the text to make it yours")}
          </p>
        )}
        <div className="trail-stage trail-guide__stage">
          <CardEditor
            key={card.id}
            t={t}
            card={card}
            position={i + 1}
            count={cards.length}
            number={nums[i]!}
            problems={added.has(card.id) ? [] : probs.filter((p) => p.card === card.id)}
            readOnly={readOnly}
            onChange={update}
            canEarlier={i > 0}
            canLater={!last}
            onEarlier={() => reorder(i, i - 1)}
            onLater={() => reorder(i, i + 1)}
            onDelete={() => remove(card.id)}
          />
          {/* Beside the card when there is room; behind a link when not. */}
          <aside className={"trail-stage__phone" + (peek ? " is-open" : "")} aria-label={t("On the teams' phones")}>
            <button type="button" className="trail-link trail-stage__peek" aria-expanded={peek} onClick={() => setPeek((v) => !v)}>
              <Icon name="phone" size={16} />
              {peek ? t("Hide the phone") : t("See it on a phone")}
            </button>
            <p className="trail-stage__caption">{t("On the teams' phones")}</p>
            <PhonePreview t={t} title={title} card={card} number={nums[i]!} />
          </aside>
        </div>
      </>,
      <>
        {backBtn(() => go(i > 0 ? { at: "card", id: cards[i - 1]!.id } : props.fresh ? { at: "name" } : { at: "overview" }))}
        <button type="button" className="trail-btn trail-guide__icon" aria-label={t("All cards")} title={t("All cards")} onClick={() => go({ at: "overview" })}>
          <Icon name="cards" size={20} />
        </button>
        {canAdd && (
          <button type="button" className="trail-btn" onClick={() => add(i + 1)}>
            <Icon name="plus" size={18} />
            {t("Card")}
          </button>
        )}
        {nextBtn(last ? t("Done") : t("Next"), () => go(last ? { at: "overview" } : { at: "card", id: cards[i + 1]!.id }))}
      </>,
      { progress: (i + 1) / cards.length },
    );
  }

  if (screen.at === "overview") {
    const unfinished = cards.flatMap((c, i) => (flagged.has(c.id) ? [{ c, i }] : []));
    const noQuestion = probs.some((p) => !p.card);
    const ready = probs.length === 0;
    return shell(
      <>
        <h1 className="trail-guide__title">{t("Your party adventure")}</h1>
        {props.notes}
        <label className="party-field">
          <span>{t("Name")}</span>
          <input value={title} maxLength={120} disabled={readOnly} onChange={(e) => onChange({ title: e.target.value })} />
        </label>
        {props.onTheme && !readOnly && (
          <div className="trail-guide__look">
            <button type="button" className={"trail-look-btn" + (looks ? " is-on" : "")} aria-expanded={looks} onClick={() => setLooks((v) => !v)}>
              <span className="trail-look-btn__swatch" aria-hidden="true" />
              <Icon name="palette" size={16} />
              {theme ? t(theme.label) : t("Look")}
            </button>
            {looks && <ThemePicker i18n={i18n} selected={props.theme} onPick={props.onTheme} />}
          </div>
        )}
        <h2 className="trail-guide__sub">{t("The cards, in order")}</h2>
        <CardList
          cards={cards}
          nums={nums}
          flagged={flagged}
          examples={readOnly ? new Set() : examples}
          readOnly={readOnly}
          onOpen={(id) => go({ at: "card", id })}
          onMove={reorder}
        />
        {!readOnly && cards.length < MAX_CARDS && (
          <button type="button" className="trail-btn trail-guide__add" onClick={() => add(cards.length)}>
            <Icon name="plus" size={18} />
            {t("Add a card")}
          </button>
        )}
        {!ready && (
          <div className="trail-needs" role="status">
            <Icon name="alert" size={18} />
            <div>
              {noQuestion && <p>{t("Add at least one question first.")}</p>}
              {unfinished.length > 0 && (
                <p>
                  {t("Finish these cards first:")}{" "}
                  {unfinished.map(({ c, i }) => (
                    <button key={c.id} type="button" className="trail-link" onClick={() => go({ at: "card", id: c.id })}>
                      {nums[i] ? t("Question {{n}}", { n: nums[i] }) : t("Information card")}
                    </button>
                  ))}
                </p>
              )}
            </div>
          </div>
        )}
      </>,
      props.finishOnOverview ? (
        <button type="button" className="trail-btn trail-btn--primary trail-guide__next" disabled={props.busy} onClick={props.finish}>
          {t("Done")}
        </button>
      ) : (
        nextBtn(t("Next"), () => go({ at: "ready" }), !ready)
      ),
      { hero: true },
    );
  }

  return shell(
    <>
      <h1 className="trail-guide__title">{t("Ready to play?")}</h1>
      <p className="trail-guide__lead">
        <strong>{title}</strong> · {t("{{n}} cards", { n: cards.length })}
      </p>
      {props.ready?.body}
    </>,
    <>
      {backBtn(back)}
      {props.ready?.action}
    </>,
    { hero: true },
  );
}

/** A drag in Overview's list, as it is drawn: the row picked up, the slot it would go to, how far it has moved. */
interface ListDrag {
  from: number;
  over: number;
  dy: number;
  /** How far the rows it passes step aside: its height and the gap. */
  shift: number;
  /** Let go: gliding into its slot. */
  dropping: boolean;
}

const DROP_MS = 180;
const EDGE_PX = 72;
const EDGE_SLOP = 24;

/**
 * Overview's cards, one row each: tap to open, drag the handle to move
 * it (on the card's screen, the arrows do the same). The row follows
 * the finger; the rows it passes slide aside to open its slot; let go,
 * it glides in, and only then does the order change, with nothing
 * left to jump. Near the screen's top or bottom the list scrolls.
 */
function CardList(props: {
  cards: Card[];
  nums: number[];
  flagged: Set<string | undefined>;
  examples: Set<string>;
  readOnly: boolean;
  onOpen: (id: string) => void;
  onMove: (from: number, to: number) => void;
}) {
  const t = useT().t;
  const list = useRef<HTMLOListElement>(null);
  const rows = useRef<(HTMLLIElement | null)[]>([]);
  const [drag, setDrag] = useState<ListDrag | null>(null);
  const [settling, setSettling] = useState(false);
  // Between pointer events: where the drag began (the rows' places then,
  // the scroll then), and where the finger is now.
  const live = useRef<{
    from: number;
    over: number;
    startY: number;
    startScroll: number;
    tops: number[];
    heights: number[];
    shift: number;
    y: number;
    scroller: HTMLElement | null;
    raf: number;
  } | null>(null);

  useEffect(() => () => cancelAnimationFrame(live.current?.raf ?? 0), []);

  const follow = () => {
    const l = live.current;
    if (!l) return;
    const dy = l.y - l.startY + (l.scroller?.scrollTop ?? 0) - l.startScroll;
    const middle = l.tops[l.from]! + l.heights[l.from]! / 2 + dy;
    let over = 0;
    l.tops.forEach((top, i) => {
      if (i !== l.from && middle > top + l.heights[i]! / 2) over++;
    });
    if (over !== l.over) {
      l.over = over;
      navigator.vibrate?.(8);
    }
    setDrag({ from: l.from, over, dy, shift: l.shift, dropping: false });
  };

  // Each frame: scroll while the finger rests near an edge (under the bar, above the foot).
  const tick = () => {
    const l = live.current;
    if (!l) return;
    const s = l.scroller;
    if (s) {
      const top = (s.querySelector(".trail-guide__bar")?.getBoundingClientRect().bottom ?? 0) + EDGE_PX;
      const bottom = (s.querySelector(".trail-guide__foot")?.getBoundingClientRect().top ?? innerHeight) - EDGE_PX;
      // Only toward where the finger has gone: a row picked up near an edge stays put.
      const by = l.y < top && l.y < l.startY - EDGE_SLOP ? l.y - top : l.y > bottom && l.y > l.startY + EDGE_SLOP ? l.y - bottom : 0;
      if (by) {
        const was = s.scrollTop;
        s.scrollTop += Math.max(-16, Math.min(16, by / 3));
        if (s.scrollTop !== was) follow();
      }
    }
    l.raf = requestAnimationFrame(tick);
  };

  const pick = (i: number, e: React.PointerEvent<HTMLElement>) => {
    if (props.readOnly || live.current || drag) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    const rects = props.cards.map((_, j) => rows.current[j]!.getBoundingClientRect());
    const gap = rects.length > 1 ? rects[1]!.top - rects[0]!.bottom : 0;
    const scroller = list.current?.closest<HTMLElement>(".trail-guide") ?? null;
    live.current = {
      from: i,
      over: i,
      startY: e.clientY,
      startScroll: scroller?.scrollTop ?? 0,
      tops: rects.map((r) => r.top),
      heights: rects.map((r) => r.height),
      shift: rects[i]!.height + gap,
      y: e.clientY,
      scroller,
      raf: 0,
    };
    navigator.vibrate?.(12);
    setDrag({ from: i, over: i, dy: 0, shift: live.current.shift, dropping: false });
    live.current.raf = requestAnimationFrame(tick);
  };

  const drop = (keep: boolean) => {
    const l = live.current;
    if (!l) return;
    cancelAnimationFrame(l.raf);
    live.current = null;
    const from = l.from;
    const over = keep ? l.over : from;
    // Where its slot is, from where it began: the rows between have stepped aside by its height.
    let dy = 0;
    if (over > from) dy = l.tops[over]! + l.heights[over]! - l.heights[from]! - l.tops[from]!;
    if (over < from) dy = l.tops[over]! - l.tops[from]!;
    setDrag({ from, over, dy, shift: l.shift, dropping: true });
    setTimeout(() => {
      // Reorder with the transitions off: every row is already where the new order puts it.
      setSettling(true);
      if (over !== from) props.onMove(from, over);
      setDrag(null);
      requestAnimationFrame(() => requestAnimationFrame(() => setSettling(false)));
    }, DROP_MS);
  };

  const place = (i: number): CSSProperties | undefined => {
    if (!drag) return undefined;
    if (i === drag.from) {
      return {
        transform: `translateY(${drag.dy}px) scale(${drag.dropping ? 1 : 1.03})`,
        transition: drag.dropping ? `transform ${DROP_MS}ms var(--trail-ease), box-shadow ${DROP_MS}ms` : "none",
      };
    }
    if (drag.from < drag.over && i > drag.from && i <= drag.over) return { transform: `translateY(${-drag.shift}px)` };
    if (drag.from > drag.over && i >= drag.over && i < drag.from) return { transform: `translateY(${drag.shift}px)` };
    return undefined;
  };

  if (props.cards.length === 0) return <p className="trail-muted">{t("No cards yet.")}</p>;
  return (
    <ol ref={list} className={"trail-guide__list" + (drag ? " is-dragging" : "") + (settling ? " is-settling" : "")}>
      {props.cards.map((c, i) => (
        <li
          key={c.id}
          ref={(el) => {
            rows.current[i] = el;
          }}
          className={drag?.from === i ? (drag.dropping ? "is-dropping" : "is-dragged") : undefined}
          style={place(i)}
        >
          {!props.readOnly && (
            <span
              className="trail-guide__grip"
              title={t("Hold and drag to move")}
              aria-hidden="true"
              onPointerDown={(e) => pick(i, e)}
              onPointerMove={(e) => {
                if (!live.current) return;
                live.current.y = e.clientY;
                follow();
              }}
              onPointerUp={() => drop(true)}
              onPointerCancel={() => drop(false)}
            >
              <Icon name="grip" size={18} />
            </span>
          )}
          <button type="button" onClick={() => props.onOpen(c.id)}>
            <span className="trail-chip__no">{props.nums[i] || <Icon name="info" size={14} />}</span>
            <span className="trail-guide__excerpt">{c.text.trim() || t("Empty")}</span>
            {props.examples.has(c.id) && (
              <span className="trail-guide__tag" title={t("Example")} aria-label={t("Example")}>
                <Icon name="pencil" size={14} />
              </span>
            )}
            {props.flagged.has(c.id) && <span className="trail-chip__flag" aria-label={t("Not finished")} />}
            <Icon name="right" size={16} />
          </button>
        </li>
      ))}
    </ol>
  );
}
