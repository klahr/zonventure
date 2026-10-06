import { useEffect, useRef, useState, type ReactNode } from "react";
import { useSyncExternalStore } from "react";
import type { I18n } from "@zonventure/i18n";
import { CHOICES, MAX_ANSWERS, MAX_CARDS, MAX_OPTION, MAX_TEXT, asKind, blank, move, numbers, problems, type Card, type Kind, type Problem } from "./cards.ts";
import type { SaveState } from "./save.ts";

type T = I18n["t"];

function useLocale(i18n: I18n) {
  useSyncExternalStore(
    (cb) => i18n.subscribe(cb),
    () => i18n.current(),
    () => i18n.current(),
  );
}

export interface EditorProps {
  i18n: I18n;
  title: string;
  cards: Card[];
  state: SaveState;
  readOnly: boolean;
  onChange: (next: { title?: string; cards?: Card[] }) => void;
  /** What sits at the right of the top bar: Done, print, the menu. */
  actions?: ReactNode;
  /** A card to select, e.g. the first with a problem after "Done". */
  focus?: { id: string; nonce: number } | null;
  /** Say what each card still needs: once the author has tried to finish. */
  showProblems?: boolean;
}

/**
 * The simple creator: the selected card, large, above a timeline of all
 * the cards. One card at a time; tap a card in the timeline to edit it,
 * hold and drag it to move it (or "Move earlier"/"Move later"), "+" to
 * add one after the selected card. Every change is the page's to save.
 */
export function Editor(props: EditorProps) {
  const { i18n, cards, readOnly, onChange } = props;
  useLocale(i18n);
  const t = i18n.t;
  const [selected, setSelected] = useState(() => cards[0]?.id ?? "");
  const [undo, setUndo] = useState<{ cards: Card[]; selected: string; label: string } | null>(null);
  const index = Math.max(0, cards.findIndex((c) => c.id === selected));
  const card = cards[index];
  const nums = numbers(cards);
  const probs = problems(cards);
  const cardProblems = card ? probs.filter((p) => p.card === card.id) : [];

  useEffect(() => {
    if (props.focus) setSelected(props.focus.id);
  }, [props.focus]);
  useEffect(() => {
    if (!undo) return;
    const id = setTimeout(() => setUndo(null), 6000);
    return () => clearTimeout(id);
  }, [undo]);

  const set = (next: Card[], keepUndo?: string) => {
    if (keepUndo) setUndo({ cards, selected, label: keepUndo });
    onChange({ cards: next });
  };
  const update = (c: Card) => set(cards.map((x) => (x.id === c.id ? c : x)));
  const add = () => {
    if (cards.length >= MAX_CARDS) return;
    const c = blank(card?.kind ?? "quiz");
    const next = [...cards];
    next.splice(card ? index + 1 : next.length, 0, c);
    set(next);
    setSelected(c.id);
  };
  const remove = () => {
    if (!card) return;
    const next = cards.filter((c) => c.id !== card.id);
    set(next, t("Card deleted."));
    setSelected(next[Math.min(index, next.length - 1)]?.id ?? "");
  };
  const reorder = (from: number, to: number) => {
    const next = move(cards, from, to);
    if (next !== cards) set(next, t("Card moved."));
  };

  return (
    <div className="trail">
      <header className="trail-bar">
        <input
          className="trail-bar__title"
          value={props.title}
          maxLength={120}
          disabled={readOnly}
          aria-label={t("Name of the quiz trail")}
          onChange={(e) => onChange({ title: e.target.value })}
        />
        <SaveNote t={t} state={props.state} />
        {props.actions && <div className="trail-bar__actions">{props.actions}</div>}
      </header>

      <div className="trail-main">
        {!card ? (
          <div className="trail-empty">
            <p>{t("No cards yet.")}</p>
            {!readOnly && (
              <button type="button" className="trail-btn trail-btn--primary" onClick={add}>
                {t("Add a card")}
              </button>
            )}
          </div>
        ) : (
          <CardEditor
            t={t}
            card={card}
            number={nums[index]!}
            problems={props.showProblems ? cardProblems : []}
            readOnly={readOnly}
            onChange={update}
            canEarlier={index > 0}
            canLater={index < cards.length - 1}
            onEarlier={() => reorder(index, index - 1)}
            onLater={() => reorder(index, index + 1)}
            onDelete={remove}
          />
        )}
      </div>

      {undo && (
        <div className="trail-undo" role="status">
          <span>{undo.label}</span>
          <button
            type="button"
            onClick={() => {
              onChange({ cards: undo.cards });
              setSelected(undo.selected);
              setUndo(null);
            }}
          >
            {t("Undo")}
          </button>
        </div>
      )}

      <Timeline t={t} cards={cards} nums={nums} problems={probs} selected={card?.id ?? ""} readOnly={readOnly} onSelect={setSelected} onMove={reorder} onAdd={add} />
    </div>
  );
}

function SaveNote({ t, state }: { t: T; state: SaveState }) {
  const text: Record<SaveState, string> = {
    saved: t("Saved"),
    saving: t("Saving…"),
    unsaved: t("Saving…"),
    failed: t("Not saved — check your connection"),
    conflict: t("Changed elsewhere — reload to see it"),
    designer: t("Changed in the designer — read only"),
  };
  return (
    <span className={"trail-save trail-save--" + state} role="status">
      {text[state]}
    </span>
  );
}

const KINDS: { kind: Kind; label: string }[] = [
  { kind: "quiz", label: "Question with options" },
  { kind: "answer", label: "Question with answer" },
  { kind: "info", label: "Information" },
];

function CardEditor(props: {
  t: T;
  card: Card;
  number: number;
  problems: Problem[];
  readOnly: boolean;
  onChange: (c: Card) => void;
  canEarlier: boolean;
  canLater: boolean;
  onEarlier: () => void;
  onLater: () => void;
  onDelete: () => void;
}) {
  const { t, card, readOnly, onChange } = props;
  const has = (code: Problem["code"]) => props.problems.some((p) => p.code === code);
  const need: Record<string, string> = {
    text: card.kind === "info" ? t("Write the text.") : t("Write the question."),
    options: t("Fill in all three options."),
    correct: t("Tap 1, X or 2 to mark the right option."),
    answers: t("Write the right answer."),
  };
  return (
    <section className="trail-card" aria-label={props.number ? t("Card {{n}}", { n: props.number }) : t("Information card")}>
      <div className="trail-kinds" role="radiogroup" aria-label={t("Kind of card")}>
        {KINDS.map((k) => (
          <button
            key={k.kind}
            type="button"
            role="radio"
            aria-checked={card.kind === k.kind}
            className={"trail-kind" + (card.kind === k.kind ? " is-on" : "")}
            disabled={readOnly}
            onClick={() => onChange(asKind(card, k.kind))}
          >
            {t(k.label)}
          </button>
        ))}
      </div>

      <div className="trail-paper">
        <span className="trail-paper__no">{props.number ? t("Card {{n}}", { n: props.number }) : t("Information")}</span>
        <textarea
          className="trail-paper__text"
          value={card.text}
          rows={3}
          maxLength={MAX_TEXT}
          disabled={readOnly}
          placeholder={card.kind === "info" ? t("Write what the teams read here…") : t("Write the question…")}
          aria-label={card.kind === "info" ? t("Text") : t("Question")}
          onChange={(e) => onChange({ ...card, text: e.target.value })}
        />

        {card.kind === "quiz" && (
          <div className="trail-options">
            {CHOICES.map((mark, i) => {
              const right = card.correct === mark;
              return (
                <div className="trail-option" key={mark}>
                  <button
                    type="button"
                    className={"trail-mark" + (right ? " is-right" : "")}
                    aria-pressed={right}
                    aria-label={t("Mark {{c}} as the right option", { c: mark })}
                    disabled={readOnly}
                    onClick={() => onChange({ ...card, correct: mark })}
                  >
                    {mark}
                  </button>
                  <input
                    value={card.options?.[i] ?? ""}
                    maxLength={MAX_OPTION}
                    disabled={readOnly}
                    placeholder={t("Option {{c}}", { c: mark })}
                    aria-label={t("Option {{c}}", { c: mark })}
                    onChange={(e) => onChange({ ...card, options: (card.options ?? ["", "", ""]).map((o, j) => (j === i ? e.target.value : o)) })}
                  />
                  {right && <span className="trail-right">{t("Right")}</span>}
                </div>
              );
            })}
          </div>
        )}

        {card.kind === "answer" && (
          <div className="trail-answers">
            {(card.answers ?? [""]).map((a, i) => (
              <div className="trail-option" key={i}>
                <input
                  value={a}
                  maxLength={MAX_OPTION}
                  disabled={readOnly}
                  placeholder={i === 0 ? t("The right answer") : t("Another accepted spelling")}
                  aria-label={i === 0 ? t("The right answer") : t("Another accepted spelling")}
                  onChange={(e) => onChange({ ...card, answers: (card.answers ?? [""]).map((x, j) => (j === i ? e.target.value : x)) })}
                />
                {i > 0 && !readOnly && (
                  <button type="button" className="trail-btn trail-btn--quiet" aria-label={t("Remove this spelling")} onClick={() => onChange({ ...card, answers: (card.answers ?? []).filter((_, j) => j !== i) })}>
                    ×
                  </button>
                )}
              </div>
            ))}
            {!readOnly && (card.answers?.length ?? 0) < MAX_ANSWERS && (
              <button type="button" className="trail-link" onClick={() => onChange({ ...card, answers: [...(card.answers ?? []), ""] })}>
                {t("+ Another accepted spelling")}
              </button>
            )}
            <p className="trail-muted">{t("The teams type the answer on their phone. Upper and lower case don't matter.")}</p>
          </div>
        )}
      </div>

      {props.problems.length > 0 && (
        <ul className="trail-needs">
          {(["text", "options", "correct", "answers"] as const).filter(has).map((code) => (
            <li key={code}>{need[code]}</li>
          ))}
        </ul>
      )}

      {!readOnly && (
        <div className="trail-card__actions">
          <button type="button" className="trail-btn" disabled={!props.canEarlier} onClick={props.onEarlier}>
            {t("Move earlier")}
          </button>
          <button type="button" className="trail-btn" disabled={!props.canLater} onClick={props.onLater}>
            {t("Move later")}
          </button>
          <button type="button" className="trail-btn trail-btn--danger" onClick={props.onDelete}>
            {t("Delete card")}
          </button>
        </div>
      )}
    </section>
  );
}

const HOLD_MS = 350;
const SLOP_PX = 8;

/**
 * The timeline: a chip per card, in order, numbered as on paper. Tap to
 * select; hold and drag to move. The strip scrolls sideways; a drag only
 * starts after holding still, so a swipe still scrolls.
 */
function Timeline(props: {
  t: T;
  cards: Card[];
  nums: number[];
  problems: Problem[];
  selected: string;
  readOnly: boolean;
  onSelect: (id: string) => void;
  onMove: (from: number, to: number) => void;
  onAdd: () => void;
}) {
  const { t, cards } = props;
  const strip = useRef<HTMLOListElement>(null);
  const chips = useRef<(HTMLLIElement | null)[]>([]);
  const press = useRef<{ i: number; x: number; y: number; timer: ReturnType<typeof setTimeout> } | null>(null);
  const [drag, setDrag] = useState<{ from: number; over: number } | null>(null);
  const dragging = useRef(false);
  dragging.current = !!drag;

  // While dragging, a touch moves the chip, not the strip.
  useEffect(() => {
    const el = strip.current;
    if (!el) return;
    const stop = (e: TouchEvent) => {
      if (dragging.current) e.preventDefault();
    };
    el.addEventListener("touchmove", stop, { passive: false });
    return () => el.removeEventListener("touchmove", stop);
  }, []);

  // The selected chip stays in view.
  useEffect(() => {
    const i = cards.findIndex((c) => c.id === props.selected);
    chips.current[i]?.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "smooth" });
  }, [props.selected, cards]);

  const overAt = (x: number) => {
    let best = 0;
    chips.current.forEach((el, i) => {
      if (!el) return;
      const r = el.getBoundingClientRect();
      if (x > r.left + r.width / 2) best = Math.min(i + 1, cards.length - 1);
      if (x >= r.left && x <= r.right) best = i;
    });
    return best;
  };

  const flagged = new Set(props.problems.map((p) => p.card));
  return (
    <nav className="trail-line" aria-label={t("Cards")}>
      <ol ref={strip} className="trail-line__strip">
        {cards.map((c, i) => {
          const on = c.id === props.selected;
          const shift = drag && drag.from !== drag.over && i === drag.over;
          return (
            <li
              key={c.id}
              ref={(el) => {
                chips.current[i] = el;
              }}
              className={"trail-chip" + (on ? " is-on" : "") + (drag?.from === i ? " is-dragged" : "") + (shift ? " is-target" : "")}
              onPointerDown={(e) => {
                if (props.readOnly) return;
                const target = e.currentTarget;
                const id = e.pointerId;
                press.current = {
                  i,
                  x: e.clientX,
                  y: e.clientY,
                  timer: setTimeout(() => {
                    target.setPointerCapture?.(id);
                    setDrag({ from: i, over: i });
                    navigator.vibrate?.(15);
                  }, HOLD_MS),
                };
              }}
              onPointerMove={(e) => {
                const p = press.current;
                if (drag) setDrag({ ...drag, over: overAt(e.clientX) });
                else if (p && Math.hypot(e.clientX - p.x, e.clientY - p.y) > SLOP_PX) {
                  clearTimeout(p.timer);
                  press.current = null;
                }
              }}
              onPointerUp={() => {
                if (press.current) clearTimeout(press.current.timer);
                press.current = null;
                if (drag) {
                  props.onMove(drag.from, drag.over);
                  props.onSelect(cards[drag.from]!.id);
                  setDrag(null);
                }
              }}
              onPointerCancel={() => {
                if (press.current) clearTimeout(press.current.timer);
                press.current = null;
                setDrag(null);
              }}
            >
              <button type="button" aria-current={on ? "true" : undefined} onClick={() => !drag && props.onSelect(c.id)}>
                <span className="trail-chip__no">{props.nums[i] || "i"}</span>
                <span className="trail-chip__text">{c.text.trim() || t("Empty")}</span>
                {flagged.has(c.id) && <span className="trail-chip__flag" aria-label={t("Not finished")} />}
              </button>
            </li>
          );
        })}
        {!props.readOnly && cards.length < MAX_CARDS && (
          <li className="trail-chip trail-chip--add">
            <button type="button" aria-label={t("Add a card")} onClick={props.onAdd}>
              +
            </button>
          </li>
        )}
      </ol>
    </nav>
  );
}
