import type { ReactNode } from "react";
import { useT } from "../../i18n.tsx";
import { fmtAgo, fmtDateTime, relTime, toDate } from "../../ui/index.ts";
import type { Activity, EventGraph, EventInfo } from "./api.ts";
import { stepTitle } from "./util.ts";

type Badge = "neutral" | "brand" | "good" | "bad";
type Info = Pick<EventInfo, "adventure_slug" | "adventure_title">;

/** "5m ago" within the day, "3 days ago" past it; the exact time is the element's title. */
export function Ago({ at }: { at: string }) {
  const t = useT();
  const loc = t.current();
  const old = Date.now() - (toDate(at)?.getTime() ?? Date.now()) > 86_400_000;
  return <time dateTime={at} title={fmtDateTime(at, loc)}>{old ? relTime(at, loc) : fmtAgo(at, t.t, loc)}</time>;
}

/** The event's activity, newest first, as readable lines. */
export function ActivityFeed({ items, event, graph }: { items: Activity[]; event: Info; graph: EventGraph | null | undefined }) {
  const t = useT();
  if (items.length === 0) return <p className="ev-muted">{t.t("No activity yet.")}</p>;
  return (
    <ul className="ev-feed">
      {items.map((e) => <ActivityLine key={e.id} e={e} event={event} graph={graph} />)}
    </ul>
  );
}

const str = (v: unknown) => (typeof v === "string" ? v : v == null ? "" : String(v));
const num = (v: unknown) => (typeof v === "number" ? v : 0);

function ActivityLine({ e, event, graph }: { e: Activity; event: Info; graph: EventGraph | null | undefined }) {
  const t = useT();
  const p = e.payload ?? {};
  const step = stepTitle(graph, str(p.node));
  const team = <strong>{e.team_name || ""}</strong>;
  let badge: Badge = "neutral", symbol = "•", meta = "";
  let msg: ReactNode;
  switch (e.kind) {
    case "event.created":
      badge = "brand"; symbol = "+";
      msg = <>{t.t("Event created for ")}<strong>{(event.adventure_slug === p.adventure_slug && event.adventure_title) || str(p.adventure_slug)}</strong></>;
      break;
    case "event.started": badge = "good"; symbol = "▶"; msg = t.t("Event started"); break;
    case "event.paused": symbol = "‖"; msg = t.t("Event paused"); break;
    case "event.resumed": badge = "good"; symbol = "▶"; msg = t.t("Event resumed"); break;
    case "event.cancelled": badge = "bad"; symbol = "✕"; msg = t.t("Event cancelled"); break;
    case "event.completed": badge = "good"; symbol = "✓"; msg = t.t("Event ended"); break;
    case "team.created": {
      badge = "brand"; symbol = "+";
      msg = <>{t.t("New team:")} <strong>{e.team_name || str(p.name)}</strong></>;
      const names = Array.isArray(p.members) ? p.members.map(str) : [];
      if (names.length) meta = t.t("with {{names}}", { names: names.join(", ") });
      break;
    }
    case "team.renamed":
      symbol = "✎"; msg = <>{t.t("Renamed ")}<strong>{str(p.from)}</strong> → <strong>{str(p.to)}</strong></>; break;
    case "team.deleted":
      badge = "bad"; symbol = "×"; msg = <>{t.t("Team deleted:")} <strong>{str(p.name)}</strong></>; break;
    case "team.member.added":
      badge = "good"; symbol = "+"; msg = <>{t.t("Added ")}<strong>{str(p.username)}</strong>{t.t(" to ")}<strong>{e.team_name || ""}</strong></>; break;
    case "team.member.removed":
      symbol = "−"; msg = <>{t.t("Removed ")}<strong>{str(p.username)}</strong>{t.t(" from ")}<strong>{e.team_name || ""}</strong></>; break;
    case "answer.wrong":
      badge = "bad"; symbol = "✗";
      msg = <>{team}{p.node ? t.t(" wrong on “{{step}}”", { step }) : t.t(" wrong on puzzle {{n}}", { n: num(p.page_index) + 1 })}</>;
      if (p.submitted) meta = t.t("submitted “{{answer}}”", { answer: str(p.submitted) });
      break;
    case "answer.right":
      badge = "good"; symbol = "✓";
      msg = <>{team}{p.node ? t.t(" solved “{{step}}”", { step }) : t.t(" solved puzzle {{n}}", { n: num(p.page_index) + 1 })}</>;
      break;
    case "team.completed": badge = "good"; symbol = "★"; msg = <>{team}{t.t(" finished the adventure!")}</>; break;
    case "team.unlocked":
      // A graph rescue names the step; a lane skip counts pages.
      badge = "brand"; symbol = "⇨";
      msg = <>{team}{p.node ? t.t(" was moved past {{step}}", { step }) : t.t(" skipped past puzzle {{n}}", { n: num(p.from_page_index) + 1 })}</>;
      if (p.by_arranger && !e.actor) meta = t.t("by arranger");
      break;
    case "gate.reached": badge = "brand"; symbol = "⛳"; msg = <>{team}{t.t(" reached the puzzle location")}</>; break;
    case "photo.taken":
      badge = "brand"; symbol = "📷"; msg = <>{team}{t.t(" sent a photo on “{{step}}”", { step })}</>; meta = t.t("waiting for review"); break;
    case "photo.reviewed": {
      const s = str(p.status);
      badge = s === "approved" ? "good" : s === "denied" ? "bad" : "neutral";
      symbol = s === "approved" ? "✓" : s === "denied" ? "✗" : "•";
      msg = <>{team}{s === "approved" ? t.t(": photo on “{{step}}” approved", { step }) : s === "denied" ? t.t(": photo on “{{step}}” denied", { step }) : t.t(": photo on “{{step}}” waiting for review again", { step })}</>;
      const pts = num(p.points);
      if (pts) meta = t.t("{{n}} points", { n: pts > 0 ? "+" + pts : pts });
      break;
    }
    default:
      msg = e.kind;
  }
  const by = e.actor ? t.t("by {{actor}}", { actor: e.actor }) : "";
  const line = [by, meta].filter(Boolean).join(" · ");
  return (
    <li className="ev-feed__item">
      <span className={`ev-feed__badge is-${badge}`} aria-hidden>{symbol}</span>
      <div className="ev-feed__text">
        <span>{msg}</span>
        {line && <span className="ev-feed__meta">{line}</span>}
      </div>
      <Ago at={e.created_at} />
    </li>
  );
}
