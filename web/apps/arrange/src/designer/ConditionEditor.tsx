import type { Cmp, Graph, Op, Predicate } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { Plus, Trash2 } from "lucide-react";
import { ICON } from "./dz/ui.tsx";

/**
 * The condition builder for Gates and hub branches.
 *
 * Conditions are a tree of plain data (internal/graph/cond), built here
 * from dropdowns, never typed as text. That is the whole point of the
 * design: no expression language to learn, no parser, no way to write
 * something that does not terminate, and every reference is a picked
 * slug the validator can check.
 */

interface Props {
  value: Predicate;
  onChange: (p: Predicate) => void;
  graph: Graph;
  depth?: number;
  /** A block's show condition, built for one phone: it may ask where "this player" is. */
  forPlayer?: boolean;
}

const OPS: { op: Op; label: string; group: string }[] = [
  { op: "", label: "Always", group: "Basic" },
  { op: "all", label: "All of…", group: "Combine" },
  { op: "any", label: "Any of…", group: "Combine" },
  { op: "not", label: "Not…", group: "Combine" },
  { op: "solved", label: "Solved a step", group: "History" },
  { op: "visited", label: "Reached a step", group: "History" },
  { op: "outcome", label: "Left a step by…", group: "History" },
  { op: "attempts", label: "Wrong guesses at a step", group: "History" },
  { op: "score", label: "Score", group: "Progress" },
  { op: "elapsed", label: "Time since start (s)", group: "Progress" },
  { op: "node_elapsed", label: "Time on this step (s)", group: "Progress" },
  { op: "team_size", label: "Team size", group: "Progress" },
  { op: "drawn", label: "Card drawn", group: "Progress" },
  { op: "item", label: "Has an item", group: "State" },
  { op: "counter", label: "Shared counter", group: "State" },
  { op: "in_zone", label: "Is inside a zone", group: "Place & time" },
  { op: "players_in_zone", label: "Players inside a zone", group: "Place & time" },
  { op: "zones_held", label: "A player in each of these zones", group: "Place & time" },
  { op: "here", label: "This player is inside a zone", group: "Place & time" },
  { op: "role", label: "This player has a role", group: "Players" },
  { op: "walked", label: "Metres walked on this step", group: "Place & time" },
  { op: "time_of_day", label: "Time of day", group: "Place & time" },
  { op: "weekday", label: "Weekday", group: "Place & time" },
];

const CMPS: { cmp: Cmp; label: string }[] = [
  { cmp: "gte", label: "≥" },
  { cmp: "gt", label: ">" },
  { cmp: "eq", label: "=" },
  { cmp: "ne", label: "≠" },
  { cmp: "lte", label: "≤" },
  { cmp: "lt", label: "<" },
];

const DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

export function ConditionEditor({ value, onChange, graph, depth = 0, forPlayer = false }: Props) {
  const t = useT();
  const set = (patch: Partial<Predicate>) => onChange({ ...value, ...patch });

  const changeOp = (op: Op) => {
    // Keep children when switching between composite kinds; otherwise
    // start the new operator clean rather than carrying stale fields.
    if (op === "not") {
      onChange({ op, of: [value.of?.[0] ?? { op: "" }] });
      return;
    }
    if (op === "all" || op === "any") {
      onChange({ op, of: value.of?.length ? value.of : [{ op: "" }] });
      return;
    }
    onChange({ op });
  };

  const steps = graph.nodes.filter((n) => n.type === "puzzle" || n.type === "story" || n.type === "hub");
  const ports = (slug?: string) => {
    const n = graph.nodes.find((x) => x.slug === slug);
    if (!n) return [];
    if (n.type === "puzzle") return (n.puzzle?.outputs ?? []).map((o) => ({ port: o.port, label: o.label || o.port }));
    if (n.type === "hub") return (n.hub?.branches ?? []).map((b) => ({ port: b.port, label: b.label || b.port }));
    return [{ port: "out", label: "continue" }];
  };

  // The steps with a random draw, and the draws and cards of one.
  const draws = (slug?: string) => {
    const n = graph.nodes.find((x) => x.slug === slug);
    return (n?.puzzle?.blocks ?? n?.story?.blocks ?? []).flatMap((b) => (b.kind === "draw" ? [b] : []));
  };
  const drawSteps = graph.nodes.filter((n) => draws(n.slug).length > 0);
  const drawBlock = draws(value.node).find((b) => b.id === value.block);

  // A gate or a branch decides for the whole team: "this player" means
  // nothing there, and the validator refuses it.
  const ops = OPS.filter((o) => !(o.op === "here" || o.op === "role") || forPlayer || value.op === o.op);
  const groups = [...new Set(ops.map((o) => o.group))];

  return (
    <div className={"cond" + (depth > 0 ? " cond--nested" : "")}>
      <select className="field__input" value={value.op} onChange={(e) => changeOp(e.target.value as Op)} aria-label={t.t("Condition")}>
        {groups.map((g) => (
          <optgroup key={g} label={t.t(g)}>
            {ops.filter((o) => o.group === g).map((o) => (
              <option key={o.op || "always"} value={o.op}>
                {t.t(o.label)}
              </option>
            ))}
          </optgroup>
        ))}
      </select>

      {(value.op === "solved" || value.op === "visited" || value.op === "outcome" || value.op === "attempts") && (
        <select className="field__input" value={value.node ?? ""} onChange={(e) => set({ node: e.target.value })} aria-label={t.t("Step")}>
          <option value="">{t.t("— pick a step —")}</option>
          {steps.map((n) => (
            <option key={n.slug} value={n.slug}>
              {n.title || n.slug}
            </option>
          ))}
        </select>
      )}

      {value.op === "outcome" && (
        <select className="field__input" value={value.is ?? ""} onChange={(e) => set({ is: e.target.value })} aria-label={t.t("Way out")}>
          <option value="">{t.t("— which way —")}</option>
          {ports(value.node).map((p) => (
            <option key={p.port} value={p.port}>
              {p.label}
            </option>
          ))}
        </select>
      )}

      {value.op === "drawn" && (
        <>
          <select
            className="field__input"
            value={value.node ?? ""}
            onChange={(e) => {
              const first = draws(e.target.value)[0]?.id;
              onChange({ op: "drawn", node: e.target.value, ...(first ? { block: first } : {}) });
            }}
            aria-label={t.t("Step")}
          >
            <option value="">{drawSteps.length ? t.t("— pick a step with a draw —") : t.t("— no step has a random draw yet —")}</option>
            {drawSteps.map((n) => (
              <option key={n.slug} value={n.slug}>
                {n.title || n.slug}
              </option>
            ))}
          </select>
          {draws(value.node).length > 1 && (
            <select className="field__input" value={value.block ?? ""} onChange={(e) => onChange({ op: "drawn", node: value.node ?? "", block: e.target.value })} aria-label={t.t("Draw")}>
              {draws(value.node).map((b, i) => (
                <option key={b.id} value={b.id}>
                  {b.text?.trim() || t.t("Draw {{n}}", { n: i + 1 })}
                </option>
              ))}
            </select>
          )}
          {drawBlock && (
            <select className="field__input" value={value.is ?? ""} onChange={(e) => set({ is: e.target.value })} aria-label={t.t("Card")}>
              <option value="">{t.t("— which card —")}</option>
              {drawBlock.options.map((o, i) => (
                <option key={i} value={String(i)}>
                  {o || t.t("Card {{n}}", { n: i + 1 })}
                </option>
              ))}
            </select>
          )}
        </>
      )}

      {value.op === "item" && (
        <select className="field__input" value={value.item ?? ""} onChange={(e) => set({ item: e.target.value })} aria-label={t.t("Item")}>
          <option value="">{t.t("— pick an item —")}</option>
          {(graph.items ?? []).map((i) => (
            <option key={i.slug} value={i.slug}>
              {i.name || i.slug}
            </option>
          ))}
        </select>
      )}

      {value.op === "counter" && (
        <select className="field__input" value={value.counter ?? ""} onChange={(e) => set({ counter: e.target.value })} aria-label={t.t("Counter")}>
          <option value="">{t.t("— pick a counter —")}</option>
          {(graph.counters ?? []).map((c) => (
            <option key={c.slug} value={c.slug}>
              {c.name || c.slug}
            </option>
          ))}
        </select>
      )}

      {(value.op === "in_zone" || value.op === "players_in_zone" || value.op === "here") && (
        <select className="field__input" value={value.zone ?? ""} onChange={(e) => set({ zone: e.target.value })} aria-label={t.t("Zone")}>
          <option value="">{t.t("— pick a zone —")}</option>
          {(graph.zones ?? []).map((z) => (
            <option key={z.slug} value={z.slug}>
              {z.name || z.slug}
            </option>
          ))}
        </select>
      )}

      {["attempts", "item", "counter", "score", "elapsed", "node_elapsed", "team_size", "players_in_zone", "walked", "time_of_day"].includes(value.op) && (
        <div className="cond__cmp">
          <select className="field__input" value={value.cmp ?? "gte"} onChange={(e) => set({ cmp: e.target.value as Cmp })} aria-label={t.t("Comparison")}>
            {CMPS.map((c) => (
              <option key={c.cmp} value={c.cmp}>
                {c.label}
              </option>
            ))}
          </select>
          {value.op === "time_of_day" ? (
            <input className="field__input" type="time" value={value.is ?? ""} onChange={(e) => set({ is: e.target.value })} aria-label={t.t("Time")} />
          ) : (
            <input
              className="field__input"
              type="number"
              value={value.n ?? (value.op === "item" || value.op === "players_in_zone" ? 1 : 0)}
              onChange={(e) => set({ n: Number(e.target.value) })}
              aria-label={t.t("Value")}
            />
          )}
        </div>
      )}

      {value.op === "weekday" && (
        <div className="cond__days">
          {DAYS.map((d) => {
            const on = (value.members ?? []).includes(d);
            return (
              <label key={d} className="chip">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() =>
                    set({ members: on ? (value.members ?? []).filter((x) => x !== d) : [...(value.members ?? []), d] })
                  }
                />
                {d}
              </label>
            );
          })}
        </div>
      )}

      {value.op === "role" && (
        <select className="field__input" value={value.role ?? ""} onChange={(e) => set({ role: e.target.value })} aria-label={t.t("Role")}>
          <option value="">{(graph.roles ?? []).length ? t.t("— pick a role —") : t.t("— no roles yet: add them under Resources —")}</option>
          {(graph.roles ?? []).map((r) => (
            <option key={r.slug} value={r.slug}>
              {r.name || r.slug}
            </option>
          ))}
        </select>
      )}

      {value.op === "zones_held" && (
        <div className="cond__days">
          {(graph.zones ?? []).length === 0 && <span className="field__hint">{t.t("— no zones yet —")}</span>}
          {(graph.zones ?? []).map((z) => {
            const on = (value.members ?? []).includes(z.slug);
            return (
              <label key={z.slug} className="chip">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() =>
                    set({ members: on ? (value.members ?? []).filter((x) => x !== z.slug) : [...(value.members ?? []), z.slug] })
                  }
                />
                {z.name || z.slug}
              </label>
            );
          })}
        </div>
      )}

      {(value.op === "all" || value.op === "any" || value.op === "not") && (
        <div className="cond__children">
          {(value.of ?? []).map((child, i) => (
            <div key={i} className="cond__child">
              <ConditionEditor
                value={child}
                graph={graph}
                depth={depth + 1}
                forPlayer={forPlayer}
                onChange={(c) => set({ of: (value.of ?? []).map((x, j) => (j === i ? c : x)) })}
              />
              {value.op !== "not" && (
                <button
                  type="button"
                  className="icon-remove"
                  onClick={() => set({ of: (value.of ?? []).filter((_, j) => j !== i) })}
                  aria-label={t.t("Remove")}
                  title={t.t("Remove")}
                >
                  <Trash2 size={ICON.sm} />
                </button>
              )}
            </div>
          ))}
          {value.op !== "not" && (
            <button type="button" className="add-btn" onClick={() => set({ of: [...(value.of ?? []), { op: "" }] })}>
              <Plus size={ICON.sm} aria-hidden="true" />
              {t.t("Add condition")}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
