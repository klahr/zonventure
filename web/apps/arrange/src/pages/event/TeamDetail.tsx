import { useEffect, useState } from "react";
import { FastForward, Hourglass, LogOut, Mail, MapPinOff, Pencil, Trash2, Undo2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, ErrorBox, ICON, Loading, Pill, useLoad } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventGraph, EventInfo, Team } from "./api.ts";
import { ActivityFeed, Ago } from "./Activity.tsx";
import { MoreMenu, type MenuItem } from "./Menu.tsx";
import { TeamAvatar, useTeamActions } from "./teamActions.tsx";
import { useNow } from "../control/model.ts";
import { canDo, fmtElapsedShort, fmtPoints, groupSeconds, isTerminal, members, stepTitle, teamProgress } from "./util.ts";

/**
 * One team: where it is, who is in it, what it has done, and what the
 * arranger can do about it — move it on, edit it, email it, withdraw it
 * or delete it. Reads the live team, so it follows pushes while open.
 */
export function TeamDetailDialog(props: {
  event: EventInfo;
  graph: EventGraph | null | undefined;
  team: Team;
  /** The server's clock less this device's, taken as the detail the team came with arrived (clockOffset). */
  offset: number;
  color: string;
  /** Changes whenever the event's activity does, to re-read this team's. */
  activityKey: number;
  reload: () => void;
  onEdit: () => void;
  onClose: () => void;
}) {
  const { event, team } = props;
  const t = useT();
  const [tick, setTick] = useState(0);
  const actions = useTeamActions(props.reload);
  // What is being moved on — "" for the team, else a group's port — so a
  // second click, or an alt-click's double, is not a second request.
  const [moving, setMoving] = useState<string | null>(null);
  const log = useLoad((s) => api.getActivity(event.id, team.id, s), [event.id, team.id, props.activityKey]);
  // Who holds which role, which group each is in while the team is split
  // up, and the groups as the organisers' rescue finds them: read again
  // as the team moves, finishes or its players change — not for every
  // change in the event, its score or rank. A read for where the team
  // stood before (at) is out of date, though it shows until the next.
  const at = `${team.id} ${team.node ?? ""} ${JSON.stringify((team.groups ?? []).map((g) => [g.port, g.node]))}`;
  const who = JSON.stringify([team.members, team.completed_at ?? ""]);
  const roster = useLoad((s) => api.getTeamRoster(team.id, s).then((r) => ({ ...r, at })), [team.id, at, who, tick]);
  // The last read for the team as it now stands — kept through a failed
  // read again, which only says so; none, as the event has the groups:
  // none to move on.
  const current = roster.data?.at === at ? roster.data : null;
  const playerOf = new Map((roster.data?.players ?? []).map((p) => [p.id, p]));
  const groups = current?.groups || team.groups || [];
  // The roster could not be read as the rescue finds the team: say so,
  // rather than leave the organiser wondering where a button went.
  const unread = (!!roster.error && !current) || current?.groups === null;
  // Until the team finishes, read the roster again now and then where
  // what the organisers may do changes with time alone: a group's branch
  // opening after a while, the time limit running out.
  const finished = teamProgress(t, props.graph, team).done;
  const ticking = !finished && (groups.length > 0 || !!props.graph?.time_limit);
  useEffect(() => {
    if (!ticking) return;
    const id = setInterval(() => setTick((n) => n + 1), 30_000);
    return () => clearInterval(id);
  }, [ticking]);
  // A time limit due and not yet run out — the team's phones gone — is the
  // organisers' to run out: the team goes where the limit says, so no move
  // of theirs is anything else meanwhile.
  const timeUp = !finished && !!current?.time_up && canDo(event, "unlock_team");
  // A team split up is moved on a group at a time, below.
  const canUnlock = !finished && !timeUp && !!team.node && !groups.length && !!props.graph?.rescuable?.includes(team.node) && canDo(event, "unlock_team");
  // A strict check: the server grants withdraw_team only while the event runs.
  const canWithdraw = (event.allowed_ops ?? []).includes("withdraw_team") && !team.completed_at;

  const where = finished ? t.t("Finished") : t.t("{{n}} points", { n: fmtPoints(team.score) });
  const more: MenuItem[] = [];
  if (team.email?.trim()) more.push({ label: t.t("Email invitation"), icon: <Mail size={ICON.sm} aria-hidden />, onClick: () => void actions.invite(team) });
  // A team not begun yet may be held at a starting point it cannot reach.
  if (!finished && !team.node && canDo(event, "unlock_team")) {
    more.push({ label: t.t("Let start anywhere"), icon: <MapPinOff size={ICON.sm} aria-hidden />, onClick: () => void actions.startAnywhere(team) });
  }
  if (canWithdraw && !isTerminal(event)) {
    more.push(team.withdrawn_at
      ? { label: t.t("Mark returned"), icon: <Undo2 size={ICON.sm} aria-hidden />, onClick: () => void actions.unwithdraw(team) }
      : { label: t.t("Mark withdrawn"), icon: <LogOut size={ICON.sm} aria-hidden />, onClick: () => void actions.withdraw(team) });
  }
  if (canDo(event, "delete_team")) {
    if (more.length) more.push({ separator: true });
    more.push({ label: t.t("Delete team"), icon: <Trash2 size={ICON.sm} aria-hidden />, danger: true, onClick: async () => { if (await actions.remove(team)) props.onClose(); } });
  }
  const step = finished ? "" : stepTitle(props.graph, team.node);

  return (
    <Dialog title={t.t("Team · {{name}}", { name: team.name })} wide onClose={props.onClose} footer={<Button autoFocus onClick={props.onClose}>{t.t("Close")}</Button>}>
      <div className="ev-td__head">
        <TeamAvatar team={team} color={props.color} size={44} />
        <div>
          <div><strong>{where}</strong>{step && <span className="cs-muted"> · {step}</span>}</div>
          <div className="ev-row-sm">
            {team.withdrawn_at && <Pill>{t.t("withdrew")}</Pill>}
            {team.cheated && <Pill tone="warn">{t.t("cheated")}</Pill>}
            {team.email && <a className="cs-small" href={"mailto:" + team.email}>{team.email}</a>}
          </div>
        </div>
      </div>
      <div className="ev-row-sm">
        {canUnlock && (
          <Button
            variant="primary"
            icon={<FastForward size={ICON.md} aria-hidden />}
            title={t.t("Alt-click to skip the confirmation")}
            disabled={moving !== null}
            loading={moving === ""}
            onClick={async (e) => {
              setMoving("");
              let moved: boolean | null = false;
              try {
                moved = await actions.unlock(team, e.altKey);
              } finally {
                setMoving(null);
              }
              if (moved) props.onClose();
              // Refused: read again as the team now stands.
              else if (moved === false) roster.reload();
            }}
          >
            {t.t("Move team on")}
          </Button>
        )}
        {timeUp && (
          <Button
            variant="primary"
            icon={<Hourglass size={ICON.md} aria-hidden />}
            title={t.t("Alt-click to skip the confirmation")}
            disabled={moving !== null}
            loading={moving === ""}
            onClick={async (e) => {
              setMoving("");
              try {
                if ((await actions.runOutTime(team, e.altKey)) === false) roster.reload();
              } finally {
                setMoving(null);
              }
            }}
          >
            {t.t("Run out the time")}
          </Button>
        )}
        <Button icon={<Pencil size={ICON.md} aria-hidden />} onClick={props.onEdit}>{t.t("Edit members")}</Button>
        {more.length > 0 && <MoreMenu label={t.t("More")} items={more} />}
      </div>
      {!finished && unread && <ErrorBox message={t.t("Couldn't read the team's players and groups.")} retry={roster.reload} retryLabel={t.t("Retry")} />}
      {!finished && groups.length ? (
        <>
          <h3 className="ev-h3">{t.t("Groups")}</h3>
          <ul className="ev-td__members">
            {groups.map((g) => (
              <li key={g.port} className="ev-td__group">
                <span>{g.label}</span>
                <span className="ev-td__group-at">
                  <span className="cs-sub">
                    {g.arrived ? t.t("waiting at {{step}}", { step: stepTitle(props.graph, g.node) }) : stepTitle(props.graph, g.node)}
                    {/* Play time on the step: since is moved past the event's ended pauses, and an open one stops the clock. */}
                    {g.since && <> · <GroupClock event={event} since={g.since} offset={props.offset} /></>}
                  </span>
                  {g.rescuable && canDo(event, "unlock_team") && (
                    <Button
                      size="sm"
                      icon={<FastForward size={ICON.sm} aria-hidden />}
                      title={t.t("Alt-click to skip the confirmation")}
                      disabled={moving !== null}
                      loading={moving === g.port}
                      onClick={async (e) => {
                        setMoving(g.port);
                        try {
                          // Moved, the team's new step reads the roster
                          // again; refused, it is read again as it stands.
                          if ((await actions.unlock(team, e.altKey, g)) === false) roster.reload();
                        } finally {
                          setMoving(null);
                        }
                      }}
                    >
                      {t.t("Move group on")}
                    </Button>
                  )}
                </span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      <h3 className="ev-h3">{t.t("Members")}</h3>
      {members(team).length === 0 ? (
        <p className="cs-muted">{t.t("No members yet.")}</p>
      ) : (
        <ul className="ev-td__members">
          {members(team).map((m) => {
            const p = playerOf.get(m.id);
            return (
              <li key={m.id}>
                <span>
                  {m.name}
                  {p?.roles.map((r) => <Pill key={r}>{r}</Pill>)}
                </span>
                <span className="cs-sub">
                  {p?.groups.length ? <>{p.groups.join(", ")} · </> : null}
                  {m.last_login_at ? <>{t.t("Last login")} <Ago at={m.last_login_at} /></> : t.t("Not joined yet")}
                </span>
              </li>
            );
          })}
        </ul>
      )}
      <h3 className="ev-h3">{t.t("Activity")}</h3>
      {log.error ? <ErrorBox message={log.error} retry={log.reload} retryLabel={t.t("Retry")} /> : !log.data ? <Loading label={t.t("Loading…")} /> : <ActivityFeed items={log.data} event={event} graph={props.graph} />}
    </Dialog>
  );
}

/** How long a group has been on its step, ticking on the server's time (offset): alone, so the dialog around it does not tick too. */
function GroupClock(props: { event: EventInfo; since: string; offset: number }) {
  const now = useNow();
  return <>{fmtElapsedShort(groupSeconds(props.event, props.since, props.offset, now) ?? 0)}</>;
}
