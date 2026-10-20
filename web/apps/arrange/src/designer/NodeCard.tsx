import { memo } from "react";
import { play } from "@zonventure/api";
import { MarkdownInline, StaticMap } from "@zonventure/play-ui";
import { STEP } from "./dz/types.tsx";
import { TRIGGER } from "./dz/concepts.tsx";
import { Bot, CircleCheck, CircleDashed, CircleDot, GitPullRequestArrow, Lightbulb, MessageSquare } from "lucide-react";
import { initials } from "../ui/kit.tsx";
import { statusLabel, type CardTalk } from "./Collab.tsx";
import { Handle, Position, type NodeProps, type Node as RFNode } from "@xyflow/react";
import { blocksOf, hasInput, mediaOf, portLabel, portsOf, textOf, type Issue, type Node, type Zone } from "@zonventure/graph";
import { useT } from "../i18n.tsx";

export interface CardData extends Record<string, unknown> {
  node: Node;
  issues: Issue[];
  /** The simulated team is here. */
  playing?: boolean;
  /** Someone else just changed it: their colour. */
  changed?: string;
  /** Who else has it open or is working on it. */
  marks?: Mark[];
  /** Someone else just added it. */
  arrived?: boolean;
  /** Someone else just removed it: a fading ghost. */
  leaving?: boolean;
  /** The work on it: status, owner, open conversations. */
  talk?: CardTalk;
  /** For a crossroads: the title of the puzzle whose answer it reads; for Give item, the item's name. */
  sourceTitle?: string;
  /** For a start with a place: its zone, drawn as a map on the card. */
  zone?: Zone;
}

export type CardNode = RFNode<CardData, "zv">;

/** Someone else on a step: open in their inspector, or an assistant's. */
export interface Mark {
  id: string;
  name: string;
  color: string;
  assistant: boolean;
}

export const TYPE_LABEL: Record<Node["type"], string> = {
  start: "Start",
  puzzle: "Puzzle",
  story: "Story",
  hub: "Hub",
  gate: "Gate",
  effect: "Effect",
  switch: "Crossroads",
  split: "Split up",
  join: "Meet again",
  end: "End",
};

/** A hub's policy, in the inspector's words. */
const POLICY_LABEL: Record<string, string> = {
  choice: "Team chooses",
  sequence: "In order",
  shuffle: "Shuffled per team",
  spread: "Least busy",
  capacity: "Capacity limits",
  random: "Random",
};

/** Ports with no label of the author's, in the console's words. */
const PORT_TEXT: Record<string, string> = { out: "next", pass: "passes", fail: "fails", done: "done", "Any other answer": "Any other answer" };

/** One line summarising what the node does, so the canvas reads without opening each card. */
function summary(n: Node, t: ReturnType<typeof useT>, sourceTitle?: string): string {
  switch (n.type) {
    case "puzzle":
      return textOf(n.puzzle?.blocks).trim() || t.t("No text yet");
    case "story":
      return textOf(n.story?.blocks).trim() || (n.story?.zone !== undefined ? t.t("Check in at a place") : t.t("No text yet"));
    case "hub": {
      const raw = n.hub?.policy ?? "choice";
      const policy = POLICY_LABEL[raw] ? t.t(POLICY_LABEL[raw]) : raw;
      const req = n.hub?.required;
      return req ? t.t("{{policy}} · any {{n}}", { policy, n: req }) : t.t("{{policy}} · all", { policy });
    }
    case "gate":
      return n.gate?.condition.op ? t.t("Conditional") : t.t("Always passes");
    case "switch": {
      const k = n.switch?.cases?.length ?? 0;
      if (!n.switch?.from) return t.t(k === 1 ? "1 path, by the answer just given" : "{{n}} paths, by the answer just given", { n: k });
      return t.t(k === 1 ? "1 path, by the answer to “{{step}}”" : "{{n}} paths, by the answer to “{{step}}”", { n: k, step: sourceTitle ?? n.switch.from });
    }
    case "effect": {
      const e = n.effect;
      if (!e) return "";
      switch (e.kind) {
        case "score":
          return t.t("{{n}} points", { n: `${(e.points ?? 0) >= 0 ? "+" : ""}${e.points ?? 0}` });
        case "grant_item":
          return t.t("Give {{n}} × {{item}}", { n: e.qty ?? 1, item: sourceTitle ?? (e.item || "?") });
        case "counter":
          return `${e.counter || "?"} ${e.op === "set" ? "=" : "+="} ${e.value ?? 0}`;
        case "journal":
          return t.t("Unlock {{entry}}", { entry: e.journal_entry || "?" });
        case "email":
          return t.t("Email {{subject}}", { subject: e.subject || "?" });
        case "webhook":
          return e.url || t.t("Webhook");
        case "theme":
          return t.t("Theme {{theme}}", { theme: e.theme || "?" });
      }
    }
    case "split": {
      const k = n.split?.paths?.length ?? 0;
      return t.t(k === 1 ? "1 group" : "{{n}} groups", { n: k });
    }
    case "join":
      return t.t("Waits for every group");
    case "end":
      return n.end?.outcome === "fail" ? t.t("Failure") : t.t("Success");
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
  const t = useT();
  const n = data.node;
  const errors = data.issues.filter((i) => i.severity === "error");
  const warnings = data.issues.filter((i) => i.severity === "warning");
  const ports = portsOf(n);
  const portIssue = (p: string) => data.issues.find((i) => i.port === p);
  const portConcept = (p: string) => {
    const trigger = n.type === "puzzle" ? n.puzzle?.outputs?.find((o) => o.port === p)?.trigger : undefined;
    return trigger ? ` concept--${TRIGGER[trigger]}` : "";
  };
  const portText = (p: string) => {
    const l = portLabel(n, p);
    return n.type === "puzzle" || !PORT_TEXT[l] ? l : t.t(PORT_TEXT[l]);
  };

  return (
    <div
      className={[
        "card",
        `card--${n.type}`,
        selected ? "is-selected" : "",
        data.playing ? "is-playing" : "",
        data.changed ? "is-changed" : "",
        data.marks?.length ? "has-peer" : "",
        data.arrived ? "is-arriving" : "",
        data.leaving ? "is-leaving" : "",
        data.talk?.unseen ? "is-unseen" : "",
        errors.length ? "has-error" : warnings.length ? "has-warning" : "",
      ].join(" ")}
      style={
        {
          ...(data.changed ? { "--changed": data.changed } : {}),
          ...(data.marks?.length ? { "--peer": data.marks[0]!.color } : {}),
        } as React.CSSProperties
      }
    >
      {!!data.marks?.length && (
        <div className="card__peers">
          {data.marks.map((m) => (
            <span key={m.id} className={"card__peer" + (m.assistant ? " is-assistant" : "")} style={{ "--peer": m.color } as React.CSSProperties}>
              {m.assistant && <Bot size={11} aria-hidden />}
              {m.name}
            </span>
          ))}
        </div>
      )}
      {hasInput(n) && <Handle type="target" position={Position.Left} id="in" className="card__in" />}

      <div className="card__head">
        <StepIcon type={n.type} />
        <span className="card__type">{t.t(TYPE_LABEL[n.type])}</span>
        {errors.length > 0 && (
          <span className="card__badge card__badge--error" title={errors.map((e) => t.t(e.message)).join("\n")}>
            {errors.length}
          </span>
        )}
        {errors.length === 0 && warnings.length > 0 && (
          <span className="card__badge card__badge--warning" title={warnings.map((e) => t.t(e.message)).join("\n")}>
            {warnings.length}
          </span>
        )}
        {data.talk?.status && <StatusIcon status={data.talk.status} label={t.t(statusLabel(data.talk.status))} />}
      </div>
      <div className="card__title">{n.title || n.slug}</div>
      {n.type !== "start" && (
        <div className="card__summary">
          {n.type === "puzzle" || n.type === "story" ? <MarkdownInline text={summary(n, t)} /> : summary(n, t, data.sourceTitle)}
        </div>
      )}
      <Thumb node={n} className="card__thumb" />
      {data.zone && <StaticMap zone={data.zone} width={216} height={112} className="card__map" />}

      {data.talk && (data.talk.open > 0 || data.talk.owner || data.talk.suggestions) && (
        <div className="card__work">
          {!!data.talk.suggestions && (
            <span className="card__suggest" title={t.t(data.talk.suggestions === 1 ? "1 suggested change" : "{{n}} suggested changes", { n: data.talk.suggestions })}>
              <GitPullRequestArrow size={10} aria-hidden />
              {data.talk.suggestions}
            </span>
          )}
          {data.talk.open > 0 && (
            <span className={"card__talk" + (data.talk.mentionsMe ? " is-mine" : "")} title={t.t(data.talk.open === 1 ? "1 open conversation" : "{{n}} open conversations", { n: data.talk.open })}>
              <MessageSquare size={10} aria-hidden />
              {data.talk.open}
            </span>
          )}
          {data.talk.owner && (
            <span className="card__owner" style={{ "--peer": data.talk.ownerColor } as React.CSSProperties} title={t.t("Owner: {{name}}", { name: data.talk.owner })}>
              {data.talk.ownerAvatar ? <img src={data.talk.ownerAvatar} alt="" /> : initials(data.talk.owner)}
            </span>
          )}
        </div>
      )}

      {ports.length > 0 && (
        <div className="card__ports">
          {ports.map((p) => (
            <div key={p} className={"card__port" + portConcept(p) + (portIssue(p) ? " is-bad" : "")}>
              <span className="card__port-label">{portText(p)}</span>
              <Handle type="source" position={Position.Right} id={p} className="card__out" />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export const NodeCard = memo(NodeCardImpl);

const STATUS_ICON = { idea: Lightbulb, draft: CircleDashed, review: CircleDot, ready: CircleCheck } as const;

/** Where a step is in the work, as a coloured icon: a card's corner, a list row's end. */
export function StatusIcon({ status, label }: { status: Exclude<CardTalk["status"], "">; label: string }) {
  const Icon = STATUS_ICON[status];
  return (
    <span className={`card__status card__status--${status}`} title={label} role="img" aria-label={label}>
      <Icon size={16} strokeWidth={2.25} aria-hidden />
    </span>
  );
}

/** The first picture on a step — or its first hint's — or nothing. */
export function firstImage(n: Node): number | undefined {
  const lists = [mediaOf(blocksOf(n)), ...(n.puzzle?.hints ?? []).map((h) => h.media)];
  for (const l of lists) {
    const m = l?.find((x) => x.kind === "image");
    if (m) return m.id;
  }
  return undefined;
}

export function Thumb({ node, className }: { node: Node; className: string }) {
  const id = firstImage(node);
  return id === undefined ? null : <img className={className} src={play.mediaURL(id)} alt="" loading="lazy" draggable={false} />;
}

function StepIcon({ type }: { type: Node["type"] }) {
  const Icon = STEP[type].icon;
  return (
    <span className={`card__icon dz-step-icon dz-step-icon--${type}`} aria-hidden="true">
      <Icon size={13} />
    </span>
  );
}
