import { useState, type ReactNode } from "react";
import { Play } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Avatar, Button, Dialog, Pill, Table, fmtDateTime, relTime, useDialogs, useToast, type Column, type Tone } from "../../ui/index.ts";
import { lookCover } from "../adventures/parts.tsx";
import { startEvent, type EventRow } from "./api.ts";

/** The event's own name, else "Event #id" — as everywhere in the console. */
export function eventLabel(e: Pick<EventRow, "id" | "name">): string {
  return e.name ? e.name : "Event #" + e.id;
}

/**
 * An event state as a pill. A scheduled event with no time set is one
 * the arranger starts by hand, so it reads "manual" rather than
 * promising a schedule.
 */
const STATE: Record<string, { tone: Tone; label: string }> = {
  running: { tone: "good", label: "running" },
  scheduled: { tone: "accent", label: "scheduled" },
  manual: { tone: "neutral", label: "manual" },
  paused: { tone: "warn", label: "paused" },
  completed: { tone: "good", label: "completed" },
  cancelled: { tone: "bad", label: "cancelled" },
};

export function StatePill({ event }: { event: Pick<EventRow, "state" | "scheduled_at"> }) {
  const t = useT();
  const key = event.state === "scheduled" && !event.scheduled_at ? "manual" : event.state;
  const s = STATE[key];
  if (!s) return <Pill>{event.state}</Pill>;
  return <Pill tone={s.tone}>{t.t(s.label)}</Pill>;
}

/** The event's adventure's icon, else its look's cover; none when it has neither. */
export function eventCover(r: EventRow): string | undefined {
  return r.adventure_avatar_url ?? lookCover(r.adventure_theme, r.adventure_cover);
}

/** The adventure's cover, or its initials on the accent colour. */
export function AdventureCover({ title, src, size = 32 }: { title: string; src?: string | undefined; size?: number }) {
  return <Avatar name={title} src={src} size={size} {...(src ? {} : { color: "var(--dz-accent)" })} />;
}

/** The moment that matters for the event's state, relative, with the exact time on hover; none for a manual start (its pill says so). */
function eventWhen(t: ReturnType<typeof useT>, r: EventRow): ReactNode {
  const loc = t.current();
  const at = r.state === "completed" ? r.completed_at : r.state === "cancelled" ? r.cancelled_at : r.state === "scheduled" ? r.scheduled_at : r.started_at;
  if (!at) return null;
  const when = relTime(at, loc);
  const text =
    r.state === "completed" ? t.t("Ended {{when}}", { when })
    : r.state === "cancelled" ? t.t("Cancelled {{when}}", { when })
    : r.state === "scheduled" ? t.t("Auto-start {{when}}", { when })
    : t.t("Started {{when}}", { when });
  return <time dateTime={at} title={fmtDateTime(at, loc)}>{text}</time>;
}

/** Live first, then upcoming soonest first, then the rest newest first. */
function byPhase(a: EventRow, b: EventRow): number {
  const rank = (r: EventRow) => (r.state === "running" || r.state === "paused" ? 0 : r.state === "scheduled" ? 1 : 2);
  const d = rank(a) - rank(b);
  if (d !== 0) return d;
  if (rank(a) === 1) return (a.scheduled_at || "\uffff").localeCompare(b.scheduled_at || "\uffff");
  const at = (r: EventRow) => r.completed_at || r.cancelled_at || r.started_at || r.scheduled_at || "";
  return at(b).localeCompare(at(a)) || b.id - a.id;
}

/** The events table: the event with its adventure and when, and its state. */
export function EventsTable({ rows, empty }: { rows: EventRow[]; empty?: ReactNode }) {
  const t = useT();
  const columns: Column<EventRow>[] = [
    { key: "event", header: t.t("Event"), cell: (r) => {
      const when = eventWhen(t, r);
      const cover = eventCover(r);
      return (
        <span className="evs-namecell">
          {cover && <AdventureCover title={r.adventure_title || r.adventure_slug} src={cover} />}
          <span className="evs-stack">
            <strong>{eventLabel(r)} {r.party && <Pill tone="accent">{t.t("Hosted")}</Pill>}</strong>
            <span className="evs-sub">
              <span>{r.adventure_title || r.adventure_slug}</span>
              {when && <><span aria-hidden>·</span>{when}</>}
            </span>
          </span>
        </span>
      );
    } },
    { key: "state", header: t.t("State"), align: "end", cell: (r) => <StatePill event={r} /> },
  ];
  return <Table columns={columns} rows={[...rows].sort(byPhase)} rowKey={(r) => r.id} rowHref={(r) => href({ name: "event", id: r.id })} empty={empty} caption={t.t("Events")} />;
}

/**
 * "Start now" for a scheduled event, with the classic console's
 * guards: an event without teams cannot start (say so, and offer the
 * event page to add them), and starting replaces a set auto-start.
 */
export function StartNowButton({ event, onStarted }: { event: EventRow; onStarted: () => void }) {
  const t = useT();
  const dialogs = useDialogs();
  const toast = useToast();
  const [noTeams, setNoTeams] = useState(false);
  const [busy, setBusy] = useState(false);
  const loc = t.current();

  async function start() {
    if (Number(event.teams_count ?? 0) === 0) {
      setNoTeams(true);
      return;
    }
    const ok = await dialogs.confirm({
      title: t.t("Start this event now?"),
      message: (
        <>
          {t.t("Starting the event opens the first puzzle to all teams. ")}
          {event.scheduled_at ? (
            <>
              {t.t("An auto-start is currently scheduled for ")}
              <strong>{fmtDateTime(event.scheduled_at, loc)}</strong>
              {t.t(". Starting now will replace that.")}
            </>
          ) : (
            t.t("You can pause it later if needed.")
          )}
        </>
      ),
      confirmLabel: t.t("Start event"),
    });
    if (!ok) return;
    setBusy(true);
    try {
      await startEvent(event.id);
      toast.ok(t.t("Started"));
      onStarted();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Button size="sm" variant="primary" icon={<Play size={14} aria-hidden />} loading={busy} onClick={() => void start()} aria-label={t.t("Start now") + " — " + eventLabel(event)}>
        {t.t("Start now")}
      </Button>
      {/* The row round this button is a link; a click inside the dialog must not reach it. */}
      {noTeams && (
        <span onClick={(e) => e.stopPropagation()}>
        <Dialog
          title={t.t("Add at least one team first")}
          onClose={() => setNoTeams(false)}
          footer={
            <>
              <Button onClick={() => setNoTeams(false)}>{t.t("OK")}</Button>
              <a className="btn primary" href={href({ name: "event", id: event.id })}>{t.t("Open event")}</a>
            </>
          }
        >
          <p>{t.t("This event has no teams yet. A team is the unit that actually plays the adventure — without one, there's nothing for the first puzzle to open to.")}</p>
        </Dialog>
        </span>
      )}
    </>
  );
}
