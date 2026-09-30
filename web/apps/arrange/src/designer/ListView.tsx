import { useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  blocksOf,
  canMove,
  gapsOf,
  insertAfter,
  insertionPoint,
  linearSteps,
  moveBy,
  moveInto,
  newNode,
  nodeBySlug,
  outline,
  removeStep,
  textOf,
  type Gap,
  type Graph,
  type Issue,
  type Node,
} from "@zonventure/graph";
import { CornerDownRight, GripVertical } from "lucide-react";
import { useT } from "../i18n.tsx";
import { Thumb } from "./NodeCard.tsx";
import { STEP } from "./dz/types.tsx";

interface Props {
  graph: Graph;
  issues: Issue[];
  selected: string | null;
  /** Where the simulated team is. */
  playing?: string | null;
  onChange: (g: Graph) => void;
  onSelect: (slug: string | null) => void;
  /** Show a step on the canvas, for what the list cannot do: wiring. */
  onOpenCanvas: (slug: string | null) => void;
}

/**
 * The list view: the adventure as an outline, for any shape of it.
 *
 * Every step appears once, in the order a team meets it, with branches
 * indented under the step they leave from and a jump where a route leads
 * back or ahead to a step listed elsewhere (see outline in
 * @zonventure/graph). It is the overview — and on a phone, where a
 * canvas cannot be worked, the way in.
 *
 * Adding a step works anywhere: after the selected one, or before the
 * End. A step with one way on can be moved into any gap in the list —
 * dragged by its handle, or a gap at a time with up and down — on the
 * main line, into a branch or out of one (see reorder in
 * @zonventure/graph). A step where the flow splits stays put, and
 * removing a step still needs a straight line; drawing a new branch is
 * the canvas's job.
 */
export function ListView({ graph, issues, selected, playing = null, onChange, onSelect, onOpenCanvas }: Props) {
  const t = useT();
  const rows = useMemo(() => outline(graph), [graph]);
  const line = useMemo(() => linearSteps(graph), [graph]);
  const gaps = useMemo(() => gapsOf(graph, rows), [graph, rows]);
  // Where up and down lead for each step that can move.
  const moves = useMemo(() => {
    const m = new Map<string, { up: Graph | null; down: Graph | null }>();
    for (const r of rows) if (r.kind === "step" && canMove(graph, r.slug)) m.set(r.slug, { up: moveBy(graph, rows, r.slug, -1), down: moveBy(graph, rows, r.slug, 1) });
    return m;
  }, [graph, rows]);

  // Dragging by the handle: the step being moved, the gaps it can go
  // to, and the one under the pointer.
  const list = useRef<HTMLOListElement>(null);
  // While dragging, the held step rides the pointer (dy) and leans to
  // the gap's depth (dx); the rows it would pass slide aside by `shift`.
  const [drag, setDrag] = useState<{ slug: string; targets: Gap[]; over: Gap | null; from: number; dx: number; dy: number; shift: number } | null>(null);
  // After a drop, the moved step glides from where it was let go.
  const settle = useRef<{ slug: string; top: number; left: number; moved: boolean } | null>(null);
  const startDrag = (e: React.PointerEvent<HTMLButtonElement>, slug: string) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const handle = e.currentTarget;
    try {
      handle.setPointerCapture(e.pointerId);
    } catch {
      // A pointer the browser no longer tracks: the drag still works while over the handle.
    }
    const targets = gaps.filter((gap) => moveInto(graph, slug, gap) !== graph);
    const from = rows.findIndex((r) => r.kind === "step" && r.slug === slug);
    const li = list.current?.querySelector<HTMLElement>(`[data-row="${from}"]`);
    if (!li) return;
    const start = li.getBoundingClientRect();
    const rowGap = parseFloat(getComputedStyle(list.current!).rowGap) || 0;
    const shift = start.height + rowGap;
    const row = rows[from];
    const depth = row && "depth" in row ? row.depth : 0;
    // One level of indent, as the stylesheet sets it for this screen.
    const indent = depth > 0 ? parseFloat(getComputedStyle(li).marginLeft) / depth : parseFloat(getComputedStyle(document.documentElement).fontSize) * (matchMedia("(max-width: 720px)").matches ? 0.9 : 1.5);
    // Where the dragged step would sit in each gap, measured once, before
    // anything slides: the gap chosen is the one nearest where it is now.
    const landing = new Map<Gap, number>();
    for (const gap of targets) {
      const el = list.current?.querySelector<HTMLElement>(`[data-row="${gap.row}"]`);
      if (!el) continue;
      const top = el.getBoundingClientRect().top;
      landing.set(gap, (gap.row > from ? top - shift : top) + start.height / 2);
    }
    const nearest = (centre: number): Gap | null => {
      let best: Gap | null = null;
      let dist = Infinity;
      for (const [gap, at] of landing) {
        const d = Math.abs(centre - at);
        if (d < dist) {
          dist = d;
          best = gap;
        }
      }
      return best;
    };
    const scroller = scrollParent(list.current);
    const startY = e.clientY;
    const startScroll = scroller?.scrollTop ?? 0;
    let y = e.clientY;
    let over: Gap | null = null;
    let frame = 0;
    const update = () => {
      const dy = y - startY + ((scroller?.scrollTop ?? 0) - startScroll);
      over = nearest(start.top + start.height / 2 + dy);
      setDrag({ slug, targets, over, from, dy, dx: over ? (over.depth - depth) * indent : 0, shift });
    };
    // Near the top or bottom of the list's scroller, scroll it along.
    const edge = () => {
      if (scroller) {
        const box = scroller.getBoundingClientRect();
        const step = y < box.top + 48 ? -8 : y > box.bottom - 48 ? 8 : 0;
        if (step) {
          scroller.scrollTop += step;
          update();
        }
      }
      frame = requestAnimationFrame(edge);
    };
    frame = requestAnimationFrame(edge);
    setDrag({ slug, targets, over, from, dy: 0, dx: 0, shift });
    const move = (ev: PointerEvent) => {
      y = ev.clientY;
      update();
    };
    const finish = (commit: boolean) => {
      cancelAnimationFrame(frame);
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      handle.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", key);
      const next = commit && over ? moveInto(graph, slug, over) : graph;
      if (next !== graph) {
        // Where the step was let go, in the window: it glides from there.
        const dx = over ? (over.depth - depth) * indent : 0;
        settle.current = { slug, top: start.top + (y - startY), left: start.left + dx, moved: true };
      }
      setDrag(null);
      if (next !== graph) onChange(next);
    };
    const up = () => finish(true);
    const cancel = () => finish(false);
    const key = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") finish(false);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
    handle.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", key);
  };
  const dropAt = (i: number) => (drag?.over?.row === i ? " is-drop" : "");
  // Where each row sits while a step is dragged: the held one on the
  // pointer, and the rows between it and its gap moved aside for it.
  const offsetOf = (i: number): React.CSSProperties | undefined => {
    if (!drag) return undefined;
    if (i === drag.from) return { transform: `translate(${drag.dx}px, ${drag.dy}px)` };
    const to = drag.over?.row;
    if (to === undefined) return undefined;
    if (to > drag.from + 1 && i > drag.from && i < to) return { transform: `translateY(${-drag.shift}px)` };
    if (to < drag.from && i >= to && i < drag.from) return { transform: `translateY(${drag.shift}px)` };
    return undefined;
  };

  // Glide a dropped step from where it was let go into its new place.
  useLayoutEffect(() => {
    const s = settle.current;
    if (!s || drag) return;
    settle.current = null;
    const items = Array.from(list.current?.querySelectorAll<HTMLElement>(":scope > li") ?? []);
    const el = items.find((x) => x.dataset.slug === s.slug);
    if (!el) return;
    const now = el.getBoundingClientRect();
    // The rows that stepped aside are already in their new places.
    for (const x of items) x.style.transition = "none";
    if (!matchMedia("(prefers-reduced-motion: reduce)").matches) el.style.transform = `translate(${s.left - now.left}px, ${s.top - now.top}px)`;
    void el.offsetHeight;
    for (const x of items) x.style.transition = "";
    el.style.transform = "";
  });

  const add = (type: "puzzle" | "story") => {
    const at = insertionPoint(graph, selected);
    if (!at) return;
    const node = newNode(graph, type, { x: 0, y: 0 });
    onChange(insertAfter(graph, at, node));
    onSelect(node.slug);
  };
  const title = (n: Node | undefined) => n?.title || (n ? t.t(STEP[n.type].label) : "");

  return (
    <div className="listview">
      <ol ref={list} className={"outline" + (drag ? " is-dragging" : "")}>
        {rows.map((r, i) => {
          if (r.kind === "tail") return <li key={`tail-${i}`} className={"outline__tail" + dropAt(i)} data-row={i} style={{ "--depth": r.depth, ...offsetOf(i) } as React.CSSProperties} aria-hidden="true" />;
          if (r.kind === "loose")
            return (
              <li key={`loose-${i}`} className="outline__heading">
                {t.t("Not connected yet — nothing leads to these")}
              </li>
            );
          const n = nodeBySlug(graph, r.slug);
          const indent = { "--depth": r.depth, ...offsetOf(i) } as React.CSSProperties;
          if (r.kind === "jump")
            return (
              <li key={`jump-${i}`} className={"outline__jump" + dropAt(i)} data-row={i} style={indent}>
                <button type="button" onClick={() => onSelect(r.slug)}>
                  <CornerDownRight size={14} aria-hidden="true" />
                  {r.via && <span className="outline__via">{r.via}</span>}
                  {t.t("continues at {{step}}", { step: title(n) })}
                </button>
              </li>
            );
          if (!n) return null;
          const kind = STEP[n.type];
          const errs = issues.filter((x) => x.node === n.slug && x.severity === "error").length;
          const at = line ? line.findIndex((s) => s.slug === n.slug) : -1;
          const mv = moves.get(n.slug);
          return (
            <li
              key={n.slug}
              className={
                "step" +
                (n.slug === selected ? " is-selected" : "") +
                (n.slug === playing ? " is-playing" : "") +
                (errs ? " has-error" : "") +
                (drag?.slug === n.slug ? " is-lifted" : "") +
                dropAt(i)
              }
              data-row={i}
              data-slug={n.slug}
              style={indent}
            >
              {mv && (
                <button
                  type="button"
                  className="step__grip"
                  onPointerDown={(e) => startDrag(e, n.slug)}
                  aria-label={t.t("Drag to move")}
                  title={t.t("Drag to move")}
                  tabIndex={-1}
                >
                  <GripVertical size={16} aria-hidden="true" />
                </button>
              )}
              <button type="button" className="step__main" onClick={() => onSelect(n.slug)}>
                <span className={`step__icon dz-step-icon dz-step-icon--${n.type}`} aria-hidden="true">
                  <kind.icon size={16} />
                </span>
                <span className="step__text">
                  {r.via && <span className="outline__via">{r.via}</span>}
                  <span className="step__title">{title(n)}</span>
                  {(() => {
                    // What it says, or else what it is — unless the title already says that.
                    const sub = summary(n) || (title(n) !== t.t(kind.label) ? t.t(kind.label) : "");
                    return sub ? <span className="step__sub">{sub}</span> : null;
                  })()}
                </span>
                <Thumb node={n} className="step__thumb" />
                {errs > 0 && <span className="card__badge card__badge--error">{errs}</span>}
              </button>
              {mv && (
                <span className="step__actions">
                  <button type="button" className="icon-btn" disabled={!mv.up} onClick={() => mv.up && onChange(mv.up)} aria-label={t.t("Move up")}>
                    ↑
                  </button>
                  <button type="button" className="icon-btn" disabled={!mv.down} onClick={() => mv.down && onChange(mv.down)} aria-label={t.t("Move down")}>
                    ↓
                  </button>
                  {line && at >= 0 && (
                  <button
                    type="button"
                    className="icon-btn"
                    onClick={() => {
                      onChange(removeStep(graph, n.slug));
                      if (selected === n.slug) onSelect(null);
                    }}
                    aria-label={t.t("Remove")}
                  >
                    ×
                  </button>
                  )}
                </span>
              )}
            </li>
          );
        })}
      </ol>
      <div className="listview__add">
        {/* In each step type's own colour and icon, as the steps in the list are. */}
        {(["puzzle", "story"] as const).map((type) => {
          const Icon = STEP[type].icon;
          return (
            <button key={type} type="button" className={`btn listview__new listview__new--${type}`} onClick={() => add(type)}>
              <span className={`dz-step-icon dz-step-icon--${type} listview__new-icon`} aria-hidden="true">
                <Icon size={14} />
              </span>
              + {t.t(type === "puzzle" ? "Puzzle" : "Story")}
            </button>
          );
        })}
        <button type="button" className="btn btn--ghost" onClick={() => onOpenCanvas(selected)}>
          {t.t("Branch it on the canvas →")}
        </button>
      </div>
      <p className="listview__hint">
        {selected ? t.t("New steps go after the selected one.") : t.t("New steps go just before the end. Select a step to add after it instead.")}
      </p>
    </div>
  );
}

function scrollParent(el: HTMLElement | null): HTMLElement | null {
  for (let p = el?.parentElement ?? null; p; p = p.parentElement) {
    const o = getComputedStyle(p).overflowY;
    if ((o === "auto" || o === "scroll") && p.scrollHeight > p.clientHeight) return p;
  }
  return null;
}

/** A line about what the step says: its prompt, text or closing words. */
function summary(n: Node): string {
  const text = n.hub?.text ?? textOf(blocksOf(n));
  return text.replace(/[#*_>`[\]]/g, "").trim().slice(0, 90);
}
