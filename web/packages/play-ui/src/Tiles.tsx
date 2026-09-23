import { useEffect, useRef, useState, type CSSProperties } from "react";
import { play } from "@zonventure/api";
import type { Translate } from "./feedback.ts";
import { guessFor, isSolved, restore, shuffled, swap, type Order } from "./tiles.ts";

/**
 * A tile puzzle: the picture cut into a grid and shuffled. Drag a tile
 * onto another to swap them, as legacy's did, or tap one and then the
 * other. When the picture is whole the order goes to the server, which
 * decides — the board only knows what it looks like.
 *
 * The order is kept on the phone after every move, so a reload or a flat
 * battery does not reshuffle a nearly finished picture. The picture is
 * cut with CSS alone: no canvas, and it is the same prefetched file the
 * offline cache already holds.
 */
export function TileBoard({
  image,
  rows,
  cols,
  storageKey,
  disabled,
  t,
  onSolved,
}: {
  image: play.PlayMedia;
  rows: number;
  cols: number;
  storageKey: string;
  disabled: boolean;
  t: Translate;
  onSolved: (guess: string) => void;
}) {
  const n = rows * cols;
  const key = `zv.tiles:${storageKey}:${image.id}:${rows}x${cols}`;
  const [order, setOrder] = useState<Order>(() => {
    try {
      return restore(JSON.parse(localStorage.getItem(key) ?? "null"), n) ?? shuffled(n);
    } catch {
      return shuffled(n);
    }
  });
  const [selected, setSelected] = useState<number | null>(null);
  const [drag, setDrag] = useState<{ slot: number; dx: number; dy: number } | null>(null);
  const [ratio, setRatio] = useState<number | null>(null);
  const board = useRef<HTMLDivElement>(null);
  const press = useRef<{ slot: number; x: number; y: number; moved: boolean } | null>(null);
  const sent = useRef(false);
  // Set when a press turned into a drag, so the click that follows it
  // does not also count as a tap.
  const dragged = useRef(false);
  const src = play.mediaURL(image.id);
  const done = isSolved(order);

  // The board takes the picture's own proportions, so no tile is squashed.
  useEffect(() => {
    const img = new Image();
    img.onload = () => setRatio(img.naturalWidth / img.naturalHeight || 1);
    img.src = src;
  }, [src]);

  const move = (a: number, b: number) => {
    if (a === b) return;
    const next = swap(order, a, b);
    setOrder(next);
    setSelected(null);
    try {
      localStorage.setItem(key, JSON.stringify(next));
    } catch {
      /* private mode: progress lasts this visit */
    }
    if (isSolved(next) && !sent.current) {
      sent.current = true;
      onSolved(guessFor(next));
    }
  };

  // The slot under a point, from the grid's geometry — the dragged tile
  // itself is under the finger, so hit-testing elements would find it.
  const slotAt = (x: number, y: number): number | null => {
    const r = board.current?.getBoundingClientRect();
    if (!r || x < r.left || x >= r.right || y < r.top || y >= r.bottom) return null;
    const c = Math.floor(((x - r.left) / r.width) * cols);
    const row = Math.floor(((y - r.top) / r.height) * rows);
    return row * cols + c;
  };

  const locked = disabled || done;

  return (
    <div className="pv-tiles-wrap">
      <div
        ref={board}
        className={"pv-tiles" + (done ? " is-solved" : "")}
        style={{ gridTemplateColumns: `repeat(${cols}, 1fr)`, gridTemplateRows: `repeat(${rows}, 1fr)`, aspectRatio: ratio ? String(ratio) : `${cols} / ${rows}` }}
        role="group"
        aria-label={t("Tile puzzle, {{rows}} by {{cols}}", { rows, cols })}
      >
        {order.map((tile, slot) => {
          const col = tile % cols;
          const row = Math.floor(tile / cols);
          const dragging = drag?.slot === slot;
          const style: CSSProperties = {
            backgroundImage: `url("${src}")`,
            backgroundSize: `${cols * 100}% ${rows * 100}%`,
            backgroundPosition: `${cols > 1 ? (col / (cols - 1)) * 100 : 0}% ${rows > 1 ? (row / (rows - 1)) * 100 : 0}%`,
            ...(dragging ? { transform: `translate(${drag.dx}px, ${drag.dy}px)`, zIndex: 2 } : {}),
          };
          return (
            <button
              key={tile}
              type="button"
              className={"pv-tile-cell" + (selected === slot ? " is-selected" : "") + (dragging ? " is-dragging" : "") + (tile === slot ? " is-home" : "")}
              style={style}
              disabled={locked}
              aria-pressed={selected === slot}
              aria-label={t("Piece {{n}}", { n: tile + 1 })}
              onPointerDown={(e) => {
                if (locked || e.button !== 0) return;
                try {
                  // Keeps the move and release coming to this tile as the
                  // finger leaves it. Throws if the pointer is already gone.
                  e.currentTarget.setPointerCapture(e.pointerId);
                } catch {
                  /* the press still works as a tap */
                }
                // A drag that ended over another tile may never have
                // produced its click; start each press clean.
                dragged.current = false;
                press.current = { slot, x: e.clientX, y: e.clientY, moved: false };
              }}
              onPointerMove={(e) => {
                const p = press.current;
                if (!p || p.slot !== slot) return;
                const dx = e.clientX - p.x;
                const dy = e.clientY - p.y;
                if (!p.moved && Math.hypot(dx, dy) < 6) return;
                p.moved = true;
                setDrag({ slot, dx, dy });
              }}
              onPointerUp={(e) => {
                const p = press.current;
                press.current = null;
                setDrag(null);
                if (!p || p.slot !== slot) return;
                dragged.current = p.moved;
                if (p.moved) {
                  const to = slotAt(e.clientX, e.clientY);
                  if (to !== null) move(slot, to);
                }
              }}
              onPointerCancel={() => {
                press.current = null;
                setDrag(null);
              }}
              onClick={() => {
                // A drag ends in a click too; only a still tap selects.
                if (dragged.current) {
                  dragged.current = false;
                  return;
                }
                if (locked) return;
                if (selected === null) setSelected(slot);
                else if (selected === slot) setSelected(null);
                else move(selected, slot);
              }}
            />
          );
        })}
      </div>
      <p className="pv-muted pv-tiles__help">
        {done ? t("The picture is whole.") : t("Drag a piece onto another to swap them — or tap one, then the other.")}
      </p>
    </div>
  );
}
