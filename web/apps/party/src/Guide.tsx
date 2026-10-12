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
  /** How many teams play free, for Ready to say. */
  freeTeams: number;
  /** Notes about the trail (a game running, reprint, ended), shown on Overview. */
  notes?: ReactNode;
  /** A passing message (copied, an error), shown on every screen. */
  note?: string | null;
  close: () => void;
  /** Done (finishOnOverview) or on to the teams and checkout (Ready). */
  finish: () => void;
  busy: boolean;
  /** What the Ready screen offers besides playing: save to email, copy, delete. */
  extras?: ReactNode;
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
    );
  }

  return shell(
    <>
      <h1 className="trail-guide__title">{t("Ready to play?")}</h1>
      <ol className="trail-guide__how">
        <li>
          <span className="trail-guide__how-icon" aria-hidden="true">
            <Icon name="print" size={26} />
          </span>
          {t("Print the cards")}
        </li>
        <li>
          <span className="trail-guide__how-icon" aria-hidden="true">
            <Icon name="hide" size={26} />
          </span>
          {t("Hide them")}
        </li>
        <li>
          <span className="trail-guide__how-icon" aria-hidden="true">
            <Icon name="phone" size={26} />
          </span>
          {t("Play on your phones")}
        </li>
      </ol>
      <p className="trail-guide__chip">
        <Icon name="users" size={18} />
        {t("Up to {{n}} teams free", { n: props.freeTeams })}
      </p>
      {props.extras}
    </>,
    <>
      {backBtn(back)}
      {nextBtn(t("Choose teams"), props.finish, props.busy)}
    </>,
    { hero: true },
  );
}

/**
 * Overview's cards, one row each: tap to open, drag the handle to move
 * it (on the card's screen, the arrows do the same).
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
  const rows = useRef<(HTMLLIElement | null)[]>([]);
  const [drag, setDrag] = useState<{ from: number; over: number } | null>(null);
  // Where the dragged row would go: past every other row whose middle is above the pointer.
  const overAt = (from: number, y: number) => {
    let n = 0;
    rows.current.forEach((el, i) => {
      if (!el || i === from) return;
      const r = el.getBoundingClientRect();
      if (y > r.top + r.height / 2) n++;
    });
    return n;
  };
  if (props.cards.length === 0) return <p className="trail-muted">{t("No cards yet.")}</p>;
  return (
    <ol className="trail-guide__list">
      {props.cards.map((c, i) => (
        <li
          key={c.id}
          ref={(el) => {
            rows.current[i] = el;
          }}
          className={(drag?.from === i ? "is-dragged" : "") + (drag && drag.from !== drag.over && drag.over === i ? " is-target" : "")}
        >
          {!props.readOnly && (
            <span
              className="trail-guide__grip"
              title={t("Hold and drag to move")}
              aria-hidden="true"
              onPointerDown={(e) => {
                e.currentTarget.setPointerCapture?.(e.pointerId);
                setDrag({ from: i, over: i });
              }}
              onPointerMove={(e) => drag && setDrag({ ...drag, over: overAt(drag.from, e.clientY) })}
              onPointerUp={() => {
                if (drag) props.onMove(drag.from, drag.over);
                setDrag(null);
              }}
              onPointerCancel={() => setDrag(null)}
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
