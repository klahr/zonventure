import { useMemo, useRef, useState } from "react";
import { Ban, Copy, Flag, LogOut, Pause, Play, Undo2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { TopBarPortal, useTopBarTitle } from "../../topbar.tsx";
import {
  Button, Callout, Card, ErrorBox, ICON, Loading, Page, PageHeader, Pill, Stat, Stats, Table,
  fmtDateTime, relTime, toDate, useDialogs, useEventWatch, useLoad, usePresence, useToast, PresenceBar, type Column,
} from "../../ui/index.ts";
import type { Message, Photo } from "../event/api.ts";
import { ActivityFeed, Ago } from "../event/Activity.tsx";
import { MoreMenu, type MenuItem } from "../event/Menu.tsx";
import { MessagesCard, PhotosCard } from "../event/Messages.tsx";
import { stateText, stateTone } from "../event/util.ts";
import { getDetail as getEventDetail } from "../event/api.ts";
import { TeamDetailDialog } from "../event/TeamDetail.tsx";
import { TeamEditDialog } from "../event/TeamEdit.tsx";
import "../event/event.css";
import {
  cancelEvent, endEvent, getActivity, getDetail, getMessages, getPhotos, getTracks, transition, unwithdrawTeam, withdrawTeam,
  type Activity, type EventDetail, type EventInfo, type Team, type Track,
} from "./api.ts";
import { ControlMap, type MapTeam } from "./ControlMap.tsx";
import { elapsedSeconds, filters, fmtElapsedShort, mapViews, QUIET_KINDS, runLabel, teamColor, teamProgress, useNow } from "./model.ts";
import "./control.css";

// Messages aren't in the pushes; a push re-reads them at most this often.
const MESSAGES_MS = 15_000;

/**
 * Race control: the live cockpit for a running event — clock, who is
 * where, what just happened, and the few actions an operator takes
 * mid-game. /ws/arrange pushes the detail, tracks and activity as they
 * change. Tapping a team opens the event page's team dialog (move on,
 * edit members, withdraw); adding teams and links stay on the event page.
 */
export function EventControlPage({ id }: { id: number }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const detail = useLoad((s) => getDetail(id, s), [id]);
  const tracks = useLoad((s) => getTracks(id, s), [id]);
  const activity = useLoad((s) => getActivity(id, s), [id]);
  // Either may be refused for a role that can still watch; the page goes on without it.
  const messages = useLoad((s) => getMessages(id, s).catch(() => ({ messages: [] as Message[] })), [id]);
  const photos = useLoad((s) => getPhotos(id, s).catch(() => ({ photos: [] as Photo[], challenges: 0 })), [id]);
  const [liveLost, setLiveLost] = useState(false);
  const [openTeam, setOpenTeam] = useState<number | null>(null);
  const [filter, setFilterState] = useState<Set<number>>(() => filters.get(id) ?? new Set());
  const msgAt = useRef(Date.now());
  const photoMark = useRef<string | null>(null);

  const d = detail.data;
  const ev = d?.event;

  const presence = usePresence();
  const watch = useEventWatch(id, (m) => {
    if (presence.take(m)) return;
    if (m.kind === "event_detail") {
      detail.set(m.data as EventDetail);
      if (Date.now() - msgAt.current > MESSAGES_MS) {
        msgAt.current = Date.now();
        messages.reload();
      }
    } else if (m.kind === "tracks") {
      tracks.set((m.data as { tracks?: Track[] | null })?.tracks ?? []);
    } else if (m.kind === "activity") {
      const list = (m.data as { activity?: Activity[] | null })?.activity ?? [];
      activity.set(list);
      // A photo sent or reviewed shows in the activity; only then re-read the photos.
      const mark = list.filter((a) => a.kind.startsWith("photo.")).map((a) => a.id).join(",");
      if (mark !== photoMark.current) {
        if (photoMark.current !== null) photos.reload();
        photoMark.current = mark;
      }
    } else if (m.kind === "watch_error") {
      setLiveLost(true);
    }
  });

  // The server's clock, against this device's, taken when the detail arrived.
  const offset = useMemo(() => {
    const s = toDate(d?.server_now)?.getTime();
    return s === undefined ? 0 : s - Date.now();
  }, [d]);

  useTopBarTitle(ev ? runLabel(ev, t) : null);

  const teams = useMemo(() => d?.teams ?? [], [d]);
  const colorOf = useMemo(() => new Map(teams.map((tm, i) => [tm.id, teamColor(i)])), [teams]);
  // A team deleted since the filter was set drops out of it.
  const active = useMemo(() => new Set([...filter].filter((fid) => teams.some((tm) => tm.id === fid))), [filter, teams]);
  const matches = (teamId: number | undefined) => active.size === 0 || teamId === undefined || active.has(teamId);
  const visibleTeams = useMemo(() => teams.filter((tm) => active.size === 0 || active.has(tm.id)), [teams, active]);
  const mapTeams = useMemo<MapTeam[]>(() => visibleTeams.map((tm) => ({ id: tm.id, name: tm.name, color: colorOf.get(tm.id)! })), [visibleTeams, colorOf]);

  const setFilter = (next: Set<number>) => {
    filters.set(id, next);
    // A new selection re-fits the map to it.
    mapViews.delete(id);
    setFilterState(next);
  };
  const toggle = (teamId: number) => {
    const next = new Set(active);
    if (next.has(teamId)) next.delete(teamId);
    else next.add(teamId);
    setFilter(next);
  };

  if (!d || !ev) {
    if (detail.error) return <Page><ErrorBox message={detail.error} retry={detail.reload} retryLabel={t.t("Retry")} /></Page>;
    return <Page><Loading label={t.t("Loading…")} /></Page>;
  }

  const refresh = () => {
    detail.reload();
    activity.reload();
  };

  const pause = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Pause this event?"),
      message: t.t("Every team sees “The game is paused” on their phone. Hint countdowns stop and answers aren't accepted until you resume.") +
        (ev.public_for_sale ? " " + t.t("Nobody can buy a ticket in the store until you resume.") : ""),
      confirmLabel: t.t("Pause"),
    });
    if (!ok) return;
    try {
      await transition(id, "pause");
      toast.ok(t.t("Paused"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  const resume = async () => {
    try {
      await transition(id, "resume");
      toast.ok(t.t("Resumed"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  const cancel = async () => {
    const pw = await dialogs.password({
      title: t.t("Cancel event?"),
      message:
        ev.state === "scheduled"
          ? t.t('The event will be marked cancelled and never start. Teams already invited keep their links but get a "cancelled" screen if they open the app. Enter your password to confirm.')
          : t.t('The event ends now. Live pings stop, the leaderboard freezes, and teams get a "cancelled" screen on their next refresh. Enter your password to confirm.'),
      confirmLabel: t.t("Cancel event"),
      cancelLabel: t.t("Keep event"),
      danger: true,
    });
    if (!pw) return;
    try {
      await cancelEvent(id, pw);
      toast.ok(t.t("Event cancelled"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  const end = async () => {
    const pw = await dialogs.password({
      title: t.t("End event?"),
      message: t.t("The event ends now for every team. Teams still playing keep the points they have, the results and recaps open, and players are signed out. Enter your password to confirm."),
      confirmLabel: t.t("End event"),
      cancelLabel: t.t("Keep it running"),
      danger: true,
    });
    if (!pw) return;
    try {
      await endEvent(id, pw);
      toast.ok(t.t("Event ended"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  const liveURL = ev.live_token && location.origin + "/live/" + encodeURIComponent(ev.live_token || "");
  const copyLive = async () => {
    try {
      await navigator.clipboard.writeText(liveURL);
      toast.ok(t.t("Live view link copied"));
    } catch {
      toast.err(t.t("Copy failed — paste from the address bar manually."));
    }
  };
  const withdraw = async (tm: Team) => {
    const ok = await dialogs.confirm({
      title: t.t("Mark “{{name}}” as withdrawn?", { name: tm.name }),
      message: t.t("They'll show in the results as withdrawn, with the points they had. You can undo this if they return."),
      confirmLabel: t.t("Mark withdrawn"),
    });
    if (!ok) return;
    try {
      await withdrawTeam(tm.id);
      toast.ok(t.t("Team marked as withdrawn"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  const unwithdraw = async (tm: Team) => {
    try {
      await unwithdrawTeam(tm.id);
      toast.ok(t.t("Team is active again"));
      refresh();
    } catch (e) {
      toast.err(e);
    }
  };

  const ended = ev.state === "completed" || ev.state === "cancelled";
  const stateLabel = ev.state === "scheduled" && !ev.scheduled_at ? "manual" : ev.state;
  const loc = t.current();
  const since = (key: string, at: string) => <span title={fmtDateTime(at, loc)}>{t.t(key, { when: relTime(at, loc) })}</span>;
  const when = ev.completed_at ? since("Ended {{when}}", ev.completed_at)
    : ev.cancelled_at ? since("Cancelled {{when}}", ev.cancelled_at)
      : ev.started_at ? since("Started {{when}}", ev.started_at)
        : t.t("Not started yet");
  const more: MenuItem[] = [];
  if (liveURL) more.push({ label: t.t("Copy live view link"), icon: <Copy size={ICON.sm} aria-hidden />, title: liveURL, onClick: () => void copyLive() });
  if (ev.state === "running" || ev.state === "paused" || ev.state === "scheduled") {
    if (more.length) more.push({ separator: true });
    // A self-guided event doesn't end: it stops selling or is cancelled.
    if (ev.state !== "scheduled" && !ev.public_for_sale) more.push({ label: t.t("End event"), icon: <Flag size={ICON.sm} aria-hidden />, danger: true, onClick: () => void end() });
    more.push({ label: t.t("Cancel event"), icon: <Ban size={ICON.sm} aria-hidden />, danger: true, onClick: () => void cancel() });
  }
  const canWithdraw = (ev.allowed_ops ?? []).includes("withdraw_team");

  const done = visibleTeams.filter((tm) => tm.completed_at).length;
  let slots = 0, claimed = 0;
  for (const tm of visibleTeams) for (const m of tm.members ?? []) {
    slots++;
    if (m.last_login_at) claimed++;
  }

  const feedItems = (activity.data ?? []).filter((a) => !QUIET_KINDS.has(a.kind) && matches(a.team_id));

  return (
    <Page wide>
      <TopBarPortal>
        <PresenceBar peers={presence.others} />
      </TopBarPortal>
      <PageHeader
        back={{ href: `#/events/${id}`, label: t.t("Back to event") }}
        title={runLabel(ev, t)}
        badges={<Pill tone={stateTone(ev.state)}>{stateText(t, stateLabel)}</Pill>}
        subtitle={<>{ev.adventure_title || ev.adventure_slug}{" · "}{when}</>}
        actions={
          <>
            {ev.state === "running" && <Button icon={<Pause size={ICON.md} aria-hidden />} onClick={pause}>{t.t("Pause")}</Button>}
            {ev.state === "paused" && <Button variant="primary" icon={<Play size={ICON.md} aria-hidden />} onClick={resume}>{t.t("Resume")}</Button>}
            {more.length > 0 && <MoreMenu label={t.t("More")} items={more} />}
          </>
        }
      />

      {liveLost && <Callout variant="danger">{t.t("Live updates stopped — you no longer have access to this event.")}</Callout>}
      {!liveLost && watch === "reconnecting" && <Callout variant="warning">{t.t("Reconnecting… Live updates resume on their own.")}</Callout>}

      {teams.length > 1 && (
        <div className="cc-filter" role="group" aria-label={t.t("Show:")}>
          <span className="cc-muted cc-small">{t.t("Show:")}</span>
          <button type="button" className={"cc-chip" + (active.size === 0 ? " is-on" : "")} aria-pressed={active.size === 0} onClick={() => setFilter(new Set())}>
            {t.t("All teams")}
          </button>
          {teams.map((tm) => (
            <button key={tm.id} type="button" className={"cc-chip" + (active.has(tm.id) ? " is-on" : "")} aria-pressed={active.has(tm.id)} onClick={() => toggle(tm.id)}>
              <span className="cc-swatch" style={{ background: colorOf.get(tm.id) }} aria-hidden />
              {tm.name}
            </button>
          ))}
        </div>
      )}

      <Stats>
        <Stat
          value={`${done}/${visibleTeams.length}`}
          label={ended ? t.t("teams finished") : visibleTeams.length === 1 ? t.t("team done") : t.t("teams done")}
          {...(visibleTeams.length > 0 && done === visibleTeams.length ? { tone: "good" as const } : {})}
        />
        <Stat value={`${claimed}/${slots}`} label={t.t("players joined")} {...(slots > 0 && claimed === slots ? { tone: "good" as const } : {})} />
        <Stat value={ev.started_at ? <Elapsed event={ev} offset={offset} /> : "—"} label={ended ? t.t("duration") : t.t("elapsed")} />
      </Stats>

      <div className="cc-grid">
        <Card title={t.t("Leaderboard")} flush>
          <Leaderboard detail={d} teams={visibleTeams} colorOf={colorOf} canWithdraw={canWithdraw} onWithdraw={withdraw} onReturn={unwithdraw} onOpen={setOpenTeam} />
        </Card>
        <Card title={t.t("Map")}>
          <ControlMap eventId={id} zones={d.graph?.zones ?? []} teams={mapTeams} tracks={tracks.data ?? []} />
        </Card>
        <Card title={t.t("Activity")}>
          <div className="cc-scroll">
            <LiveFeed items={feedItems} detail={d} />
          </div>
        </Card>
        {messages.data && <MessagesCard event={ev} teams={teams} messages={messages.data.messages ?? []} reload={messages.reload} />}
      </div>

      <PhotosCard event={ev} graph={d.graph} photos={photos.data?.photos ?? []} challenges={photos.data?.challenges ?? 0} reload={() => { photos.reload(); detail.reload(); }} />
      {openTeam !== null && <TeamDialog eventId={id} teamId={openTeam} color={colorOf.get(openTeam) ?? ""} onClose={() => setOpenTeam(null)} />}
    </Page>
  );
}

/** The event's running time, ticking; frozen while paused and once it ends. */
function Elapsed({ event, offset }: { event: EventInfo; offset: number }) {
  const now = useNow();
  return <>{fmtElapsedShort(elapsedSeconds(event, offset, now))}</>;
}

/** The event page's feed, re-rendered on the clock so "5m ago" keeps up. */
function LiveFeed({ items, detail }: { items: Activity[]; detail: EventDetail }) {
  useNow();
  return <ActivityFeed items={items} event={detail.event} graph={detail.graph} />;
}

const joined = (tm: Team) => !!tm.completed_at || (tm.members ?? []).some((m) => m.last_login_at);

/** The team's latest sign of life: its last logged activity or a player signing in, whichever is later. */
function LastSeen({ tm, labelled }: { tm: Team; labelled?: boolean }) {
  const t = useT();
  if (!joined(tm)) return <>{t.t("Not joined yet")}</>;
  const at = [tm.last_activity_at, ...(tm.members ?? []).map((m) => m.last_login_at)]
    .reduce<string | undefined>((a, b) => ((toDate(b)?.getTime() ?? 0) > (toDate(a)?.getTime() ?? 0) ? b : a), undefined);
  if (!at) return <>—</>;
  return labelled ? <>{t.t("Last activity")}{" "}<TickingAgo at={at} /></> : <TickingAgo at={at} />;
}

function TickingAgo({ at }: { at: string }) {
  useNow();
  return <Ago at={at} />;
}

function Leaderboard(props: {
  detail: EventDetail;
  teams: Team[];
  colorOf: Map<number, string>;
  canWithdraw: boolean;
  onWithdraw: (tm: Team) => void;
  onReturn: (tm: Team) => void;
  onOpen: (teamId: number) => void;
}) {
  const t = useT();
  const { detail } = props;
  // The server's rank by points (level teams share it).
  const rows = [...props.teams].sort((a, b) => (a.rank || Infinity) - (b.rank || Infinity) || a.name.localeCompare(b.name, undefined, { numeric: true }));
  if (rows.length === 0) {
    return <p className="cc-muted cc-pad">{props.teams.length === 0 && (detail.teams ?? []).length === 0 ? t.t("No teams yet.") : t.t("No teams match the current filter.")}</p>;
  }
  const columns: Column<Team>[] = [
    { key: "rank", header: "#", width: "3rem", cell: (tm) => <span className="cc-mono">{tm.rank ? "#" + tm.rank : "—"}</span> },
    {
      key: "team",
      header: t.t("Team"),
      cell: (tm) => (
        <div className={tm.withdrawn_at ? "cc-withdrawn" : undefined}>
          <span className="cc-team">
            <span className="cc-swatch" style={{ background: props.colorOf.get(tm.id) }} aria-hidden />
            <button type="button" className="cc-team__name cc-team__open" onClick={() => props.onOpen(tm.id)}>{tm.name}</button>
            {tm.completed_at && <Pill tone="good">{"✓ " + t.t("done")}</Pill>}
            {tm.withdrawn_at && <Pill>{t.t("withdrew")}</Pill>}
            {tm.cheated && <Pill tone="warn">{t.t("cheated")}</Pill>}
          </span>
          <span className="cc-narrow cc-muted cc-small"><LastSeen tm={tm} labelled /></span>
        </div>
      ),
    },
    {
      key: "progress",
      header: t.t("Progress"),
      cell: (tm) => {
        const p = teamProgress(detail, tm, t);
        return (
          <div className={tm.withdrawn_at ? "cc-withdrawn" : undefined}>
            <div className="cc-mono">{p.main}</div>
            {p.step && <div className="cc-muted cc-small">{p.step}</div>}
          </div>
        );
      },
    },
    { key: "last", header: t.t("Last activity"), wide: true, cell: (tm) => <span className="cc-muted cc-small"><LastSeen tm={tm} /></span> },
  ];
  if (props.canWithdraw) columns.push(
    {
      key: "actions",
      header: <span className="cs-sr">{t.t("Actions")}</span>,
      align: "end",
      cell: (tm) =>
        // A finisher can't be withdrawn after the fact; the server refuses once the event has ended.
        props.canWithdraw && !tm.completed_at ? (
          tm.withdrawn_at ? (
            <Button size="icon" variant="ghost" title={t.t("Mark returned")} aria-label={`${t.t("Mark returned")}: ${tm.name}`} icon={<Undo2 size={ICON.md} aria-hidden />} onClick={() => props.onReturn(tm)} />
          ) : (
            <Button size="icon" variant="ghost" title={t.t("Mark withdrawn")} aria-label={`${t.t("Mark withdrawn")}: ${tm.name}`} icon={<LogOut size={ICON.md} aria-hidden />} onClick={() => props.onWithdraw(tm)} />
          )
        ) : null,
    },
  );
  return (
    <div className="cc-scroll">
      <Table columns={columns} rows={rows} rowKey={(tm) => tm.id} caption={t.t("Leaderboard")} />
    </div>
  );
}

/**
 * The event page's team dialog, over race control. It reads the event
 * page's fuller detail, which the pushed race-control detail lacks.
 */
function TeamDialog({ eventId, teamId, color, onClose }: { eventId: number; teamId: number; color: string; onClose: () => void }) {
  const full = useLoad((s) => getEventDetail(eventId, s), [eventId]);
  const [editing, setEditing] = useState(false);
  const team = full.data?.teams?.find((tm) => tm.id === teamId);
  if (!full.data || !team) return null;
  if (editing) return <TeamEditDialog key={team.id} event={full.data.event} team={team} color={color} reload={full.reload} onClose={onClose} />;
  return (
    <TeamDetailDialog
      event={full.data.event}
      graph={full.data.graph}
      team={team}
      color={color}
      activityKey={0}
      reload={full.reload}
      onEdit={() => setEditing(true)}
      onClose={onClose}
    />
  );
}
