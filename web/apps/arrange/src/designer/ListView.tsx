import { appendStep, moveStep, removeStep, type Graph, type Issue, type Node } from "@zonventure/graph";
import { useT } from "../i18n.tsx";

interface Props {
  graph: Graph;
  steps: Node[];
  issues: Issue[];
  selected: string | null;
  onChange: (g: Graph) => void;
  onSelect: (slug: string | null) => void;
  onOpenCanvas: () => void;
}

/**
 * The list view, for adventures that are a straight line.
 *
 * Most adventures are: puzzle, puzzle, puzzle, finish. A canvas is the
 * wrong tool for that — it makes an author draw wires to express "and
 * then". This shows the same graph as an ordered list, with reordering
 * done by rewiring underneath, and hands over to the canvas the moment
 * the author wants a branch.
 */
export function ListView({ graph, steps, issues, selected, onChange, onSelect, onOpenCanvas }: Props) {
  const t = useT();
  const add = (type: "puzzle" | "story") => {
    const r = appendStep(graph, type);
    onChange(r.graph);
    if (r.slug) onSelect(r.slug);
  };

  return (
    <div className="listview">
      <ol className="listview__steps">
        {steps.map((s, i) => {
          const errs = issues.filter((x) => x.node === s.slug && x.severity === "error").length;
          return (
            <li key={s.slug} className={"step" + (s.slug === selected ? " is-selected" : "") + (errs ? " has-error" : "")}>
              <button type="button" className="step__main" onClick={() => onSelect(s.slug)}>
                <span className="step__num">{i + 1}</span>
                <span className="step__text">
                  <span className="step__title">{s.title || s.slug}</span>
                  <span className="step__sub">
                    {s.type === "story" ? t.t("Story") : s.puzzle?.prompt?.slice(0, 90) || t.t("No prompt yet")}
                  </span>
                </span>
                {errs > 0 && <span className="card__badge card__badge--error">{errs}</span>}
              </button>
              <span className="step__actions">
                <button type="button" className="icon-btn" disabled={i === 0} onClick={() => onChange(moveStep(graph, s.slug, -1))} aria-label={t.t("Move up")}>
                  ↑
                </button>
                <button
                  type="button"
                  className="icon-btn"
                  disabled={i === steps.length - 1}
                  onClick={() => onChange(moveStep(graph, s.slug, 1))}
                  aria-label={t.t("Move down")}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="icon-btn"
                  onClick={() => {
                    onChange(removeStep(graph, s.slug));
                    if (selected === s.slug) onSelect(null);
                  }}
                  aria-label={t.t("Remove")}
                >
                  ×
                </button>
              </span>
            </li>
          );
        })}
      </ol>
      {steps.length === 0 && <p className="lede">{t.t("No steps yet. Add the first puzzle.")}</p>}
      <div className="listview__add">
        <button type="button" className="btn btn--primary" onClick={() => add("puzzle")}>
          + {t.t("Puzzle")}
        </button>
        <button type="button" className="btn btn--ghost" onClick={() => add("story")}>
          + {t.t("Story")}
        </button>
        <button type="button" className="btn btn--ghost" onClick={onOpenCanvas}>
          {t.t("Branch it on the canvas →")}
        </button>
      </div>
    </div>
  );
}
