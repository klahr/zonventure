import { useCallback, useEffect, useRef, useState } from "react";
import { Compass, Pencil } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href } from "../../router.ts";
import { TopBarPortal, useTopBarTitle } from "../../topbar.tsx";
import { Button, Callout, Card, ErrorBox, fmtDateTime, ICON, Loading, Page, PageHeader, Pill, PresenceBar, relTime, useEventWatch, useLoad, usePresence } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { Activity, EventDetail } from "./api.ts";
import { ActivityFeed } from "./Activity.tsx";
import { LifecycleActions, RenameDialog } from "./Lifecycle.tsx";
import { MessagesCard, PhotosCard } from "./Messages.tsx";
import { NewTeamsDialog } from "./NewTeams.tsx";
import { ConnectBanner, QuickStats, ResultsCard, SecretRolesCard, StoreCard } from "./Overview.tsx";
import { TeamDetailDialog } from "./TeamDetail.tsx";
import { TeamEditDialog } from "./TeamEdit.tsx";
import { TeamsCard } from "./Teams.tsx";
import { EmailsCard } from "./Emails.tsx";
import { StatisticsCard } from "./Statistics.tsx";
import { isQuest, runLabel, stateLabel, stateText, stateTone, teamColor } from "./util.ts";
import "./event.css";

type Open = null | { kind: "rename" } | { kind: "new-team" } | { kind: "team"; id: number } | { kind: "edit-team"; id: number };

/**
 * An event's page: setting it up beforehand and wrapping it up after —
 * the live map and leaderboard are race control's. It follows the event
 * live: /ws/arrange pushes its detail and activity whenever anything in
 * it changes.
 */
export function EventPage({ id }: { id: number }) {
  const t = useT();
  const { state } = useSession();
  const detail = useLoad((s) => api.getDetail(id, s), [id]);
  const activity = useLoad((s) => api.getActivity(id, undefined, s), [id]);
  const messages = useLoad((s) => api.getMessages(id, s), [id]);
  const photos = useLoad((s) => api.getPhotos(id, s), [id]);
  // Statistics come from play, once it has begun.
  const played = !!detail.data?.event.started_at;
  const stats = useLoad((s) => (played ? api.getStatistics(id, s) : Promise.resolve(null)), [id, played]);
  const slug = detail.data?.event.adventure_slug;
  const adventure = useLoad((s) => (slug ? api.getAdventure(slug, s) : Promise.resolve(null)), [slug]);
  const quest = detail.data ? isQuest(detail.data.event) : false;
  const orgId = detail.data?.event.org_id;
  // Who the event's emails come from, for their preview.
  const orgName = (state.status === "authenticated" && state.user.memberships.find((m) => m.org_id === orgId)?.org_name) || "Zonventure";
  // A failed status read shows no banner rather than blocking the page.
  const connect = useLoad((s) => (quest && orgId ? api.getConnectStatus(orgId, s).catch(() => null) : Promise.resolve(null)), [quest, orgId]);
  const [open, setOpen] = useState<Open>(null);
  const [watchLost, setWatchLost] = useState(false);
  const [activityKey, setActivityKey] = useState(0);
  const now = useNow(30_000);

  // Messages, photos and statistics aren't pushed; a burst of activity re-reads them once.
  const sideTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const { reload: reloadMessages } = messages;
  const { reload: reloadPhotos } = photos;
  const { reload: reloadStats } = stats;
  useEffect(() => () => clearTimeout(sideTimer.current), []);
  const presence = usePresence();
  useEventWatch(id, (m) => {
    if (presence.take(m)) return;
    if (m.kind === "watch_error") return setWatchLost(true);
    if (m.kind === "event_detail" && m.data) detail.set(m.data as EventDetail);
    if (m.kind === "activity") {
      const list = (m.data as { activity?: Activity[] } | undefined)?.activity;
      if (list) activity.set(list);
      setActivityKey((k) => k + 1);
      clearTimeout(sideTimer.current);
      sideTimer.current = setTimeout(() => { reloadMessages(); reloadPhotos(); reloadStats(); }, 1500);
    }
  });

  const reload = useCallback(() => {
    detail.reload();
    activity.reload();
    setActivityKey((k) => k + 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detail.reload, activity.reload]);

  const name = detail.data ? runLabel(detail.data.event) : null;
  useTopBarTitle(name);

  if (!detail.data) {
    return (
      <Page>
        {detail.error ? <ErrorBox message={detail.error} retry={detail.reload} retryLabel={t.t("Retry")} /> : <Loading label={t.t("Loading…")} />}
      </Page>
    );
  }
  if (state.status !== "authenticated") return null;
  const user = state.user;
  const { event, graph } = detail.data;
  const teams = detail.data.teams ?? [];
  const adv = adventure.data ?? null;
  const label = stateLabel(event);
  const index = new Map(teams.map((tm, i) => [tm.id, i]));
  const loc = t.current();
  const when = event.completed_at
    ? t.t("Ended {{when}}", { when: fmtDateTime(event.completed_at, loc) })
    : event.cancelled_at
      ? t.t("Cancelled {{when}}", { when: fmtDateTime(event.cancelled_at, loc) })
      : event.started_at
        ? t.t("Started {{when}}", { when: fmtDateTime(event.started_at, loc) })
        : event.scheduled_at
          ? t.t("Auto-start {{when}}", { when: fmtDateTime(event.scheduled_at, loc) }) + " · " + relTime(event.scheduled_at, loc)
          : t.t("Not scheduled — starts when you start it");
  const openTeam = open && (open.kind === "team" || open.kind === "edit-team") ? teams.find((tm) => tm.id === open.id) : undefined;

  return (
    <Page>
      <TopBarPortal>
        <PresenceBar peers={presence.others} />
      </TopBarPortal>
      <div className="ev-head">
        <PageHeader
          back={quest ? { href: href({ name: "quests" }), label: t.t("Self-guided adventures") } : { href: href({ name: "events" }), label: t.t("Events") }}
          title={
            <>
              {name}
              <Button variant="ghost" size="icon" aria-label={t.t("Rename event")} title={t.t("Rename event")} icon={<Pencil size={ICON.md} aria-hidden />} onClick={() => setOpen({ kind: "rename" })} />
            </>
          }
          badges={
            <>
              {quest && <Pill tone="accent" title={t.t("Listed in your store; tickets sold to the public.")}><Compass size={ICON.xs} aria-hidden />{t.t("Self-guided adventure")}</Pill>}
              <Pill tone={stateTone(event.state)}>{stateText(t, label)}</Pill>
            </>
          }
          subtitle={
            <span className="ev-sub">
              <a href={href({ name: "adventure", slug: event.adventure_slug })}>{adv?.title || event.adventure_title || event.adventure_slug}</a>
              <span aria-hidden>·</span>
              <span>{when}</span>
            </span>
          }
          actions={<LifecycleActions event={event} teams={teams} adventure={adv} user={user} reload={reload} />}
        />
      </div>

      {watchLost && <Callout variant="danger">{t.t("Live updates stopped — you no longer have access to this event.")}</Callout>}

      <QuickStats event={event} teams={teams} now={now} />
      {quest && <ConnectBanner event={event} status={connect.data ?? null} user={user} />}
      {quest && <StoreCard key={event.id} event={event} onSaved={detail.reload} />}
      {event.has_secret_roles && <SecretRolesCard key={`secret-${event.id}`} event={event} onSaved={detail.reload} />}
      <ResultsCard event={event} teams={teams} />
      {played && <StatisticsCard stats={stats.data} error={stats.error} retry={stats.reload} />}

      <div className="ev-cols">
        <div className="ev-col">
          <TeamsCard
            event={event}
            graph={graph}
            teams={teams}
            now={now}
            reload={reload}
            onOpen={(tid) => setOpen({ kind: "team", id: tid })}
            onEdit={(tid) => setOpen({ kind: "edit-team", id: tid })}
            onNew={() => setOpen({ kind: "new-team" })}
          />
        </div>
        <div className="ev-col">
          <EmailsCard event={event} teams={teams} from={orgName} />
          {messages.data && <MessagesCard event={event} teams={teams} messages={messages.data.messages ?? []} reload={messages.reload} />}
          {photos.data && <PhotosCard event={event} graph={graph} photos={photos.data.photos ?? []} challenges={photos.data.challenges ?? 0} reload={() => { photos.reload(); reload(); }} />}
          <Card title={t.t("Event log")}>
            <div className="ev-scroll">
              {activity.error ? <ErrorBox message={activity.error} retry={activity.reload} retryLabel={t.t("Retry")} /> : !activity.data ? <Loading label={t.t("Loading…")} /> : <ActivityFeed items={activity.data} event={event} graph={graph} />}
            </div>
          </Card>
        </div>
      </div>

      {open?.kind === "rename" && <RenameDialog event={event} onClose={() => setOpen(null)} onDone={reload} />}
      {open?.kind === "new-team" && <NewTeamsDialog eventId={event.id} teams={teams} onClose={() => setOpen(null)} onDone={reload} />}
      {open?.kind === "team" && openTeam && (
        <TeamDetailDialog
          event={event}
          graph={graph}
          team={openTeam}
          color={teamColor(index.get(openTeam.id) ?? 0)}
          activityKey={activityKey}
          reload={reload}
          onEdit={() => setOpen({ kind: "edit-team", id: openTeam.id })}
          onClose={() => setOpen(null)}
        />
      )}
      {open?.kind === "edit-team" && openTeam && (
        <TeamEditDialog key={openTeam.id} event={event} team={openTeam} color={teamColor(index.get(openTeam.id) ?? 0)} reload={reload} onClose={() => setOpen(null)} />
      )}
    </Page>
  );
}

/** The time now, ticking every `ms`, for clocks and the stuck check between pushes. */
function useNow(ms: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(id);
  }, [ms]);
  return now;
}
