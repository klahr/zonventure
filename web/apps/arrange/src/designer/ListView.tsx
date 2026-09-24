import { useMemo } from "react";
import {
  blocksOf,
  insertAfter,
  insertionPoint,
  linearSteps,
  moveStep,
  newNode,
  nodeBySlug,
  outline,
  removeStep,
  textOf,
  type Graph,
  type Issue,
  type Node,
} from "@zonventure/graph";
import { CornerDownRight } from "lucide-react";
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
 * End. Moving and removing need a straight line, where "up" and "down"
 * mean something; drawing a new branch is the canvas's job.
 */
export function ListView({ graph, issues, selected, playing = null, onChange, onSelect, onOpenCanvas }: Props) {
  const t = useT();
  const rows = useMemo(() => outline(graph), [graph]);
  const line = useMemo(() => linearSteps(graph), [graph]);

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
      <ol className="outline">
        {rows.map((r, i) => {
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
              <li key={`jump-${i}`} className="outline__jump" style={indent}>
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
          return (
            <li
              key={n.slug}
              className={"step" + (n.slug === selected ? " is-selected" : "") + (n.slug === playing ? " is-playing" : "") + (errs ? " has-error" : "")}
              style={indent}
            >
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
              {line && at >= 0 && (
                <span className="step__actions">
                  <button type="button" className="icon-btn" disabled={at === 0} onClick={() => onChange(moveStep(graph, n.slug, -1))} aria-label={t.t("Move up")}>
                    ↑
                  </button>
                  <button
                    type="button"
                    className="icon-btn"
                    disabled={at === line.length - 1}
                    onClick={() => onChange(moveStep(graph, n.slug, 1))}
                    aria-label={t.t("Move down")}
                  >
                    ↓
                  </button>
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

/** A line about what the step says: its prompt, text or closing words. */
function summary(n: Node): string {
  const text = n.hub?.text ?? textOf(blocksOf(n));
  return text.replace(/[#*_>`[\]]/g, "").trim().slice(0, 90);
}
