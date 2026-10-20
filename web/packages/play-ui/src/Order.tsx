import { useRef, useState, type CSSProperties } from "react";
import { play } from "@zonventure/api";
import { isHex, readableOn } from "./brand.ts";
import type { Translate } from "./feedback.ts";
import { dealt, dropPlace, guessFor, makesWay, moveCard, restore, type Stack } from "./order.ts";

type Card = NonNullable<play.PlayInput["cards"]>[number];

/**
 * An order puzzle: a stack of cards, each text on a colour or a picture,
 * dragged into order and then tried. Only the server knows the right
 * order, so a wrong one gets the wrong-answer reply and the cards stay
 * where the team put them, to adjust.
 *
 * A finger drags a card by its grip, so the rest of the card still
 * scrolls the page past a long stack; a mouse drags it from anywhere.
 * Tapping one card and then another moves the first to the second's
 * place, and a focused card moves with the arrow keys. The stack is kept
 * on the phone after every move, as a tile picture's is.
 */
export function OrderBoard({
  cards,
  storageKey,
  disabled,
  label,
  t,
  onTry,
}: {
  cards: Card[];
  storageKey: string;
  disabled: boolean;
  /** The try button's words while it is disabled for a reason the team should read: "Not there yet". */
  label?: string | undefined;
  t: Translate;
  onTry: (guess: string) => void;
}) {
  const n = cards.length;
  // The cards themselves are in the key, so a stack saved before the
  // author changed them is not laid over the new ones.
  const key = `zv.order:${storageKey}:${cards.map((c) => c.text ?? c.image?.id ?? "").join("\u0001")}`;
  const [stack, setStack] = useState<Stack>(() => {
    try {
      return restore(JSON.parse(localStorage.getItem(key) ?? "null"), n) ?? dealt(n);
    } catch {
      return dealt(n);
    }
  });
  const [selected, setSelected] = useState<number | null>(null);
  const [drag, setDrag] = useState<{ from: number; dy: number; to: number; step: number } | null>(null);
  const list = useRef<HTMLOListElement>(null);
  const press = useRef<{ place: number; y: number; mids: number[]; step: number; moved: boolean; el: HTMLElement; pointer: number } | null>(null);
  // Set when a press turned into a drag, so the click that follows it
  // does not also count as a tap.
  const dragged = useRef(false);

  const move = (from: number, to: number) => {
    setSelected(null);
    if (from === to) return;
    const next = moveCard(stack, from, to);
    setStack(next);
    try {
      localStorage.setItem(key, JSON.stringify(next));
    } catch {
      /* private mode: the order lasts this visit */
    }
  };

  // Every card's middle and the distance from one card to the next, as
  // the drag begins: the cards making way move by that much.
  const measure = () => {
    const items = Array.from(list.current?.children ?? []) as HTMLElement[];
    const rects = items.map((el) => el.getBoundingClientRect());
    const mids = rects.map((r) => r.top + r.height / 2);
    const step = rects.length > 1 ? rects[1]!.top - rects[0]!.top : (rects[0]?.height ?? 0);
    return { mids, step };
  };

  const startPress = (e: React.PointerEvent, place: number) => {
    if (disabled || e.button !== 0) return;
    press.current = { place, y: e.clientY, ...measure(), moved: false, el: e.currentTarget as HTMLElement, pointer: e.pointerId };
  };
  const pressMove = (e: React.PointerEvent) => {
    const p = press.current;
    if (!p) return;
    const dy = e.clientY - p.y;
    if (!p.moved && Math.abs(dy) < 6) return;
    if (!p.moved) {
      try {
        // Keeps the move and release coming here as the finger leaves the
        // card. Only once it is a drag: captured from the press, a still
        // click would land on the card rather than its button. Throws if
        // the pointer is already gone.
        p.el.setPointerCapture(p.pointer);
      } catch {
        /* the drag follows the card while the pointer stays on it */
      }
    }
    p.moved = true;
    setDrag({ from: p.place, dy, to: dropPlace(p.mids, p.place, p.mids[p.place]! + dy), step: p.step });
  };
  const pressEnd = () => {
    const p = press.current;
    press.current = null;
    if (p?.moved && drag) move(drag.from, drag.to);
    dragged.current = !!p?.moved;
    setDrag(null);
  };
  const pressCancel = () => {
    press.current = null;
    setDrag(null);
  };

  return (
    <div className="pv-order-wrap">
      <ol ref={list} className={"pv-order" + (drag ? " is-dragging" : "")} aria-label={t("Cards to put in order")}>
        {stack.map((card, place) => {
          const c = cards[card]!;
          const name = c.text || c.image?.alt || t("Card {{n}}", { n: place + 1 });
          const dragging = drag?.from === place;
          const shift = drag && !dragging ? makesWay(place, drag.from, drag.to) * drag.step : 0;
          const bg = isHex(c.color) ? c.color : undefined;
          const style: CSSProperties = {
            ...(bg ? { background: bg, color: readableOn(bg) } : {}),
            ...(dragging ? { transform: `translateY(${drag.dy}px)`, zIndex: 2 } : shift ? { transform: `translateY(${shift}px)` } : {}),
          };
          return (
            <li
              key={card}
              className={"pv-order__card" + (c.image ? " has-image" : "") + (selected === place ? " is-selected" : "") + (dragging ? " is-dragging" : "")}
              style={style}
              onPointerDown={(e) => {
                // A drag by the grip ends with no click on the card to
                // clear this; start each press clean.
                dragged.current = false;
                if (e.pointerType === "mouse") startPress(e, place);
              }}
              onPointerMove={pressMove}
              onPointerUp={pressEnd}
              onPointerCancel={pressCancel}
            >
              {c.image && <img className="pv-order__bg" src={play.mediaURL(c.image.id)} alt="" draggable={false} />}
              <button
                type="button"
                className="pv-order__body"
                disabled={disabled}
                aria-pressed={selected === place}
                aria-label={t("{{card}}, place {{n}} of {{of}}", { card: name, n: place + 1, of: n })}
                onClick={() => {
                  // A drag ends in a click too; only a still tap selects.
                  if (dragged.current) {
                    dragged.current = false;
                    return;
                  }
                  if (selected === null) setSelected(place);
                  else move(selected, place);
                }}
                onKeyDown={(e) => {
                  if (e.key === "ArrowUp" && place > 0) {
                    e.preventDefault();
                    move(place, place - 1);
                  } else if (e.key === "ArrowDown" && place < n - 1) {
                    e.preventDefault();
                    move(place, place + 1);
                  }
                }}
              >
                <span className="pv-order__place" aria-hidden>
                  {place + 1}
                </span>
                {c.text && <span className="pv-order__text">{c.text}</span>}
              </button>
              <span
                className="pv-order__grip"
                aria-hidden
                onPointerDown={(e) => {
                  // The card's own handler takes a mouse; this one, a finger or pen.
                  if (e.pointerType !== "mouse") startPress(e, place);
                }}
              >
                ⠿
              </span>
            </li>
          );
        })}
      </ol>
      <p className="pv-muted pv-order__help">
        {selected !== null ? t("Now tap where it goes.") : t("Drag a card by ⠿ to move it — or tap one, then where it goes.")}
      </p>
      <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={disabled} onClick={() => onTry(guessFor(stack))}>
        {label || t("Try it")}
      </button>
    </div>
  );
}
