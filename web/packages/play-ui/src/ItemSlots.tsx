import { useState } from "react";
import type { play } from "@zonventure/api";
import type { Translate } from "./feedback.ts";

/**
 * Slots filled from the backpack: pick a slot, then the item to put in
 * it. A filled slot, picked, trades places with the next slot tapped —
 * or moves into it when that one is empty — is emptied by tapping it
 * again, and takes a backpack item in place of its own. The phone knows
 * only the slots' names — which item fits where is the server's — so
 * the fill goes up as an answer like any other, and a wrong one gets the
 * wrong-answer reply and stays as placed, to adjust. An item can go in
 * as many slots as the team holds of it.
 */
export function ItemSlotsBoard({
  input,
  items,
  disabled,
  t,
  onTry,
  onLeave,
}: {
  input: play.PlayInput;
  items: play.ItemView[];
  disabled: boolean;
  t: Translate;
  onTry: (guess: string) => void;
  /** Take the puzzle's route for a team without the items, when the server offers it. */
  onLeave: () => void;
}) {
  const slots = input.slots ?? [];
  const [placed, setPlaced] = useState<(string | null)[]>(() => slots.map(() => null));
  const [picked, setPicked] = useState<number | null>(null);
  // The slot an item goes in: the one picked, else the first empty one.
  const target = picked ?? placed.findIndex((p) => p === null);
  const left = (slug: string) => (items.find((i) => i.slug === slug)?.qty ?? 0) - placed.filter((p) => p === slug).length;
  const item = (slug: string) => items.find((i) => i.slug === slug);
  const put = (slug: string) => {
    if (target < 0) return;
    // Into the slot, and whatever was there back to the backpack.
    setPlaced((p) => p.map((x, i) => (i === target ? slug : x)));
    setPicked(null);
  };
  const tapSlot = (i: number) => {
    if (picked === null) {
      setPicked(i);
    } else if (picked === i) {
      // Tapped again: a filled slot gives its item back.
      if (placed[i] !== null) setPlaced((p) => p.map((x, j) => (j === i ? null : x)));
      setPicked(null);
    } else if (placed[picked] === null && placed[i] === null) {
      setPicked(i);
    } else {
      // Two slots, one of them filled: they trade what they hold.
      setPlaced((p) => p.map((x, j) => (j === picked ? p[i]! : j === i ? p[picked]! : x)));
      setPicked(null);
    }
  };
  const full = placed.every((p) => p !== null);

  return (
    <div className="pv-lock-wrap">
      <div className="pv-slots" role="group" aria-label={t("Item slots")}>
        {slots.map((label, i) => {
          const it = placed[i] ? item(placed[i]!) : undefined;
          const name = label || t("Slot {{n}}", { n: i + 1 });
          return (
            <button
              key={i}
              type="button"
              className={"pv-slot" + (it ? " is-filled" : "") + (picked === i || (picked === null && target === i) ? " is-target" : "")}
              disabled={disabled}
              onClick={() => tapSlot(i)}
              aria-label={it ? t("{{slot}}: {{item}}", { slot: name, item: it.name || it.slug }) : t("{{slot}}: empty", { slot: name })}
              aria-pressed={picked === i}
            >
              <span className="pv-slot__face" aria-hidden>
                {it ? it.icon || "▣" : ""}
              </span>
              <span className="pv-slot__label">{it ? it.name || it.slug : name}</span>
            </button>
          );
        })}
      </div>

      {input.missing && (
        <p className="pv-slots__missing" role="note">
          {input.missing_items?.length
            ? t("You're missing: {{items}}.", { items: input.missing_items.join(", ") })
            : t("You're missing something for this.")}
        </p>
      )}

      {picked !== null && placed[picked] !== null && (
        <p className="pv-slots__hint" role="status">{t("Tap another slot to swap, or this one again to take it out.")}</p>
      )}

      {items.length === 0 ? (
        <p className="pv-slots__empty">{t("Your backpack is empty.")}</p>
      ) : (
        <div className="pv-slots__bag" role="group" aria-label={t("Your backpack")}>
          {items.map((it) => {
            const n = left(it.slug);
            return (
              <button key={it.slug} type="button" className="pv-slots__item" disabled={disabled || n <= 0 || target < 0} onClick={() => put(it.slug)}>
                {it.icon && <span aria-hidden>{it.icon} </span>}
                {it.name || it.slug}
                {it.qty > 1 && <span className="pv-slots__qty"> ×{n}</span>}
              </button>
            );
          })}
        </div>
      )}

      <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={disabled || !full} onClick={() => onTry(placed.join("|"))}>
        {t("Try it")}
      </button>
      {input.leave && (
        <button type="button" className="pv-btn pv-btn--wide" disabled={disabled} onClick={onLeave}>
          {input.leave_label || t("Come back later")}
        </button>
      )}
    </div>
  );
}
