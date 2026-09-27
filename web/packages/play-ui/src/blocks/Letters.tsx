import { useEffect, useMemo, useRef, useState, type PointerEvent } from "react";
import { letterTiles, moveTile, shuffleTiles } from "../gadgets.ts";
import type { Renderer } from "./context.ts";
import "./gadgets.css";

interface Tile {
  id: number;
  ch: string;
}

/**
 * Letter tiles to move about while working out an anagram: dragged
 * with a finger, or moved with the arrow keys. Only this phone sees how
 * they lie; nothing is graded.
 */
export const LettersBlock: Renderer<"letters"> = ({ block, ctx }) => {
  const { t } = ctx;
  const start = useMemo(() => letterTiles(block.text ?? "").map((ch, id) => ({ id, ch })), [block.text]);
  const [tiles, setTiles] = useState<Tile[]>(() => shuffleTiles(start));
  const [held, setHeld] = useState<number | null>(null);
  const row = useRef<HTMLDivElement>(null);

  // A new text from the author, in the designer: lay out the new letters.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    setTiles(shuffleTiles(start));
  }, [start]);

  const focus = (i: number) => requestAnimationFrame(() => row.current?.querySelectorAll<HTMLElement>(".pv-letters__tile")[i]?.focus());
  const move = (from: number, to: number) => setTiles((ts) => moveTile(ts, from, to));

  const drag = (e: PointerEvent<HTMLDivElement>) => {
    if (held == null) return;
    const el = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>("[data-tile]");
    const to = el ? Number(el.dataset["tile"]) : -1;
    if (to >= 0 && to !== held) {
      move(held, to);
      setHeld(to);
    }
  };

  if (!tiles.length) return null;
  return (
    <figure className="pv-letters">
      <div ref={row} className="pv-letters__row" role="list" aria-label={t("Letter tiles")} onPointerMove={drag} onPointerUp={() => setHeld(null)} onPointerCancel={() => setHeld(null)}>
        {tiles.map((tile, i) => (
          <button
            key={tile.id}
            type="button"
            role="listitem"
            data-tile={i}
            className={"pv-letters__tile" + (held === i ? " is-held" : "")}
            aria-label={t("{{letter}}, tile {{n}} of {{count}}. Arrow keys move it.", { letter: tile.ch, n: i + 1, count: tiles.length })}
            onPointerDown={(e) => {
              e.currentTarget.releasePointerCapture?.(e.pointerId);
              setHeld(i);
            }}
            onKeyDown={(e) => {
              const to = e.key === "ArrowLeft" || e.key === "ArrowUp" ? i - 1 : e.key === "ArrowRight" || e.key === "ArrowDown" ? i + 1 : null;
              if (to == null || to < 0 || to >= tiles.length) return;
              e.preventDefault();
              move(i, to);
              focus(to);
            }}
          >
            {tile.ch}
          </button>
        ))}
      </div>
      <figcaption className="pv-gadget__caption">
        <span className="pv-gadget__note">{t("Drag the letters about. Only you see how they lie.")}</span>
      </figcaption>
    </figure>
  );
};
