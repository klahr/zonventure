import { memo } from "react";
import { Handle, Position, type NodeProps, type Node as RFNode } from "@xyflow/react";
import { hasInput, portLabel, portsOf, type Issue, type Node } from "@zonventure/graph";

export interface CardData extends Record<string, unknown> {
  node: Node;
  issues: Issue[];
}

export type CardNode = RFNode<CardData, "zv">;

const TYPE_LABEL: Record<Node["type"], string> = {
  start: "Start",
  puzzle: "Puzzle",
  story: "Story",
  hub: "Hub",
  gate: "Gate",
  effect: "Effect",
  end: "End",
};

/** One line summarising what the node does, so the canvas reads without opening each card. */
function summary(n: Node): string {
  switch (n.type) {
    case "puzzle":
      return n.puzzle?.prompt?.trim() || "No prompt yet";
    case "story":
      return n.story?.text?.trim() || (n.story?.zone !== undefined ? "Check in at a place" : "No text yet");
    case "hub": {
      const policy = n.hub?.policy ?? "choice";
      const req = n.hub?.required;
      return req ? `${policy} · any ${req}` : `${policy} · all`;
    }
    case "gate":
      return n.gate?.condition.op ? "Conditional" : "Always passes";
    case "effect": {
      const e = n.effect;
      if (!e) return "";
      switch (e.kind) {
        case "score":
          return `${(e.points ?? 0) >= 0 ? "+" : ""}${e.points ?? 0} points`;
        case "grant_item":
          return `Give ${e.qty ?? 1} × ${e.item || "?"}`;
        case "counter":
          return `${e.counter || "?"} ${e.op === "set" ? "=" : "+="} ${e.value ?? 0}`;
        case "journal":
          return `Unlock ${e.journal_entry || "?"}`;
        case "email":
          return `Email ${e.template || "?"}`;
        case "webhook":
          return e.url || "Webhook";
        case "theme":
          return `Theme ${e.theme || "?"}`;
      }
    }
    case "end":
      return n.end?.outcome === "fail" ? "Failure" : "Success";
    default:
      return "";
  }
}

/**
 * A node on the canvas: a header with its type and title, a one-line
 * summary, and one labelled output handle per port. Modelled on
 * dazyflow's NodeCard, trimmed to what an adventure needs — no OAuth
 * banners, runners or approval bars.
 *
 * Outputs sit on the right and the input on the left, so the flow reads
 * left to right, matching Tidy.
 */
function NodeCardImpl({ data, selected }: NodeProps<CardNode>) {
  const n = data.node;
  const errors = data.issues.filter((i) => i.severity === "error");
  const warnings = data.issues.filter((i) => i.severity === "warning");
  const ports = portsOf(n);
  const portIssue = (p: string) => data.issues.find((i) => i.port === p);

  return (
    <div
      className={[
        "card",
        `card--${n.type}`,
        selected ? "is-selected" : "",
        errors.length ? "has-error" : warnings.length ? "has-warning" : "",
      ].join(" ")}
    >
      {hasInput(n) && <Handle type="target" position={Position.Left} id="in" className="card__in" />}

      <div className="card__head">
        <span className="card__type">{TYPE_LABEL[n.type]}</span>
        {errors.length > 0 && (
          <span className="card__badge card__badge--error" title={errors.map((e) => e.message).join("\n")}>
            {errors.length}
          </span>
        )}
        {errors.length === 0 && warnings.length > 0 && (
          <span className="card__badge card__badge--warning" title={warnings.map((e) => e.message).join("\n")}>
            {warnings.length}
          </span>
        )}
      </div>
      <div className="card__title">{n.title || n.slug}</div>
      {n.type !== "start" && <div className="card__summary">{summary(n)}</div>}

      {ports.length > 0 && (
        <div className="card__ports">
          {ports.map((p) => (
            <div key={p} className={"card__port" + (portIssue(p) ? " is-bad" : "")}>
              <span className="card__port-label">{portLabel(n, p)}</span>
              <Handle type="source" position={Position.Right} id={p} className="card__out" />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export const NodeCard = memo(NodeCardImpl);
