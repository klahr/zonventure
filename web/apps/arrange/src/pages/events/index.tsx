import { useState } from "react";
import { Map as MapIcon, Plus } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Card, Empty, ErrorBox, Loading, Page, PageHeader, Table, fmtMoney, useLoad, type Column } from "../../ui/index.ts";
import { SPOTS } from "@zonventure/ui";
import { listAdventures, listAssignments, listEvents, type EventRow } from "./api.ts";
import { AdventureCover, EventsTable, StatePill, eventCover, eventLabel } from "./parts.tsx";
import { NewEventDialog, canStartEvent, type NewEventProps } from "./NewEventDialog.tsx";
import { NewQuestDialog } from "./NewQuestDialog.tsx";
import { useJustPurchased } from "./justPurchased.ts";
import "./events.css";

export { NewEventDialog, type NewEventProps } from "./NewEventDialog.tsx";
export { useJustPurchased } from "./justPurchased.ts";
export { EventsTable, StatePill, eventLabel } from "./parts.tsx";

type Filter = "all" | "running" | "paused" | "scheduled" | "completed" | "cancelled";

/**
 * The empty events list. With an adventure ready to run, its button
 * schedules the first event, picking the adventure in the dialog; with
 * none, it says an adventure comes first and goes to them.
 */
export function NoEvents({ onSchedule }: { onSchedule?: (() => void) | undefined }) {
  const t = useT();
  if (onSchedule) {
    return (
      <Empty
        title={t.t("No events yet.")}
        illustration={SPOTS.noEvents}
        action={<Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={onSchedule}>{t.t("Schedule event")}</Button>}
      >
        {t.t("Pick an adventure to schedule your first event — you can add teams and share player links once it's created.")}
      </Empty>
    );
  }
  return (
    <Empty
      title={t.t("No events yet.")}
      illustration={SPOTS.noEvents}
      action={<a className="btn primary" href={href({ name: "adventures" })}><MapIcon size={16} aria-hidden />{t.t("Go to Adventures")}</a>}
    >
      {t.t("An event runs a published adventure. Make or publish one first, then schedule it here.")}
    </Empty>
  );
}

/**
 * Events: the org's regular, operator-run events. Self-guided ones are
 * on their own page, so a live race isn't lost among always-on quests.
 */
export function EventsPage() {
  const t = useT();
  const events = useLoad((signal) => listEvents(false, signal, false), []);
  const ready = useLoad(async (signal) => {
    const [advs, assigns] = await Promise.all([listAdventures(signal), listAssignments(signal)]);
    return canStartEvent(advs, assigns);
  }, []);
  const [filter, setFilter] = useState<Filter>("all");
  const [dialog, setDialog] = useState<Omit<NewEventProps, "onClose"> | null>(null);
  useJustPurchased((assignmentId) => setDialog({ assignmentId }));

  const rows = events.data ?? [];
  const count = (f: Filter) => (f === "all" ? rows.length : rows.filter((r) => r.state === f).length);
  // Only the states that have events; no filters at all when there is one state.
  const filters = ([
    { key: "running", label: t.t("Live") },
    { key: "paused", label: t.t("Paused") },
    { key: "scheduled", label: t.t("Scheduled") },
    { key: "completed", label: t.t("Completed") },
    { key: "cancelled", label: t.t("Cancelled") },
  ] as { key: Filter; label: string }[]).filter((f) => count(f.key) > 0);
  const active = filters.some((f) => f.key === filter) ? filter : "all";
  const shown = active === "all" ? rows : rows.filter((r) => r.state === active);

  return (
    <Page>
      <PageHeader
        title={t.t("Events")}
        actions={ready.data ? <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setDialog({})}>{t.t("Schedule event")}</Button> : undefined}
      />
      {events.error && !events.data ? (
        <ErrorBox message={events.error} retry={events.reload} retryLabel={t.t("Retry")} />
      ) : !events.data ? (
        <Loading label={t.t("Loading…")} />
      ) : rows.length === 0 ? (
        <NoEvents onSchedule={ready.data ? () => setDialog({}) : undefined} />
      ) : (
        <>
          {filters.length > 1 && (
            <div className="evs-filters" role="group" aria-label={t.t("Filter by state")}>
              {[{ key: "all" as const, label: t.t("All") }, ...filters].map((f) => (
                <Button key={f.key} size="sm" variant={active === f.key ? "primary" : "ghost"} aria-pressed={active === f.key} onClick={() => setFilter(f.key)}>
                  {f.label}
                  <span className="evs-count">{count(f.key)}</span>
                </Button>
              ))}
            </div>
          )}
          <Card flush>
            <EventsTable rows={shown} empty={<p className="evs-none">{t.t("No events in this state.")}</p>} />
          </Card>
        </>
      )}
      {dialog && <NewEventDialog {...dialog} onClose={() => setDialog(null)} />}
    </Page>
  );
}

/**
 * Self-guided adventures: public_for_sale events, sold per ticket in
 * the org's store and played whenever a team likes.
 */
export function QuestsPage() {
  const t = useT();
  const quests = useLoad((signal) => listEvents(true, signal), []);
  const [creating, setCreating] = useState(false);
  const loc = t.current();
  const newBtn = (
    <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setCreating(true)}>
      {t.t("New self-guided adventure")}
    </Button>
  );

  const columns: Column<EventRow>[] = [
    { key: "name", header: t.t("Self-guided adventure"), cell: (r) => (
      <span className="evs-namecell">
        {eventCover(r) && <AdventureCover title={r.adventure_title || r.adventure_slug} src={eventCover(r)} />}
        <span className="evs-stack">
          <strong>{eventLabel(r)}</strong>
          <span className="evs-sub">{r.adventure_title || r.adventure_slug}</span>
        </span>
      </span>
    ) },
    // Teams sold, over the pass's cap when there is one.
    { key: "tickets", header: t.t("Tickets"), wide: true, align: "end", cell: (r) => (r.max_players > 0 ? `${r.teams_count} / ${r.max_players}` : String(r.teams_count)) },
    { key: "price", header: t.t("Price"), align: "end", cell: (r) => (r.ticket_price_cents && r.ticket_price_cents > 0 ? fmtMoney(r.ticket_price_cents, r.ticket_currency || "EUR", loc) : <span className="evs-muted">—</span>) },
    { key: "state", header: t.t("State"), align: "end", cell: (r) => <StatePill event={r} /> },
  ];

  return (
    <Page>
      <PageHeader
        title={t.t("Self-guided adventures")}
        subtitle={t.t("Sold in your store: each ticket bought creates a team, and teams play whenever they like.")}
        {...(quests.data && quests.data.length > 0 ? { actions: newBtn } : {})}
      />
      {quests.error && !quests.data ? (
        <ErrorBox message={quests.error} retry={quests.reload} retryLabel={t.t("Retry")} />
      ) : !quests.data ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        quests.data.length === 0 ? (
          <Empty title={t.t("No self-guided adventures yet.")} action={newBtn} illustration={SPOTS.noSelfGuided} />
        ) : (
          <Card flush>
            <Table columns={columns} rows={quests.data} rowKey={(r) => r.id} rowHref={(r) => href({ name: "event", id: r.id })} caption={t.t("Self-guided adventures")} />
          </Card>
        )
      )}
      {creating && <NewQuestDialog onClose={() => setCreating(false)} onCreated={quests.reload} />}
    </Page>
  );
}
