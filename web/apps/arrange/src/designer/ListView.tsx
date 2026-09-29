import { useMemo, useRef, useState } from "react";
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
  const [drag, setDrag] = useState<{ slug: string; targets: Gap[]; over: Gap | null } | null>(null);
  const startDrag = (e: React.PointerEvent<HTMLButtonElement>, slug: string) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const handle = e.currentTarget;
    handle.setPointerCapture(e.pointerId);
    const targets = gaps.filter((gap) => moveInto(graph, slug, gap) !== graph);
    let over: Gap | null = null;
    setDrag({ slug, targets, over });
    const scroller = scrollParent(list.current);
    const move = (ev: PointerEvent) => {
      if (scroller) {
        const box = scroller.getBoundingClientRect();
        if (ev.clientY < box.top + 48) scroller.scrollBy(0, -16);
        else if (ev.clientY > box.bottom - 48) scroller.scrollBy(0, 16);
      }
      over = nearestGap(list.current, targets, ev.clientY);
      setDrag({ slug, targets, over });
    };
    const finish = (commit: boolean) => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      handle.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", key);
      setDrag(null);
      if (commit && over) onChange(moveInto(graph, slug, over));
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
          if (r.kind === "tail") return <li key={`tail-${i}`} className={"outline__tail" + dropAt(i)} data-row={i} style={{ "--depth": r.depth } as React.CSSProperties} aria-hidden="true" />;
          if (r.kind === "loose")
            return (
              <li key={`loose-${i}`} className="outline__heading">
                {t.t("Not connected yet — nothing leads to these")}
              </li>
            );
          const n = nodeBySlug(graph, r.slug);
          const indent = { "--depth": r.depth } as React.CSSProperties;
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
        <button type="button" className="btn btn--primary" onClick={() => add("puzzle")}>
          + {t.t("Puzzle")}
        </button>
        <button type="button" className="btn btn--ghost" onClick={() => add("story")}>
          + {t.t("Story")}
        </button>
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

/**
 * The gap nearest the pointer. A gap sits in the space above its row;
 * a tail row has no height, so its gap is the space below the row
 * before it — where the tail row itself is.
 */
function nearestGap(list: HTMLOListElement | null, targets: Gap[], y: number): Gap | null {
  if (!list) return null;
  let best: Gap | null = null;
  let dist = Infinity;
  for (const gap of targets) {
    const el = list.querySelector<HTMLElement>(`[data-row="${gap.row}"]`);
    if (!el) continue;
    const box = el.getBoundingClientRect();
    const d = Math.abs(y - box.top);
    if (d < dist) {
      dist = d;
      best = gap;
    }
  }
  return best;
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
