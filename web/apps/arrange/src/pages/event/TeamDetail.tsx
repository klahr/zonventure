import { FastForward, LogOut, Mail, MapPinOff, Pencil, Trash2, Undo2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, ErrorBox, ICON, Loading, Pill, useLoad } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventGraph, EventInfo, Team } from "./api.ts";
import { ActivityFeed, Ago } from "./Activity.tsx";
import { MoreMenu, type MenuItem } from "./Menu.tsx";
import { TeamAvatar, useTeamActions } from "./teamActions.tsx";
import { canDo, fmtPoints, isTerminal, members, stepTitle, teamProgress } from "./util.ts";

/**
 * One team: where it is, who is in it, what it has done, and what the
 * arranger can do about it — move it on, edit it, email it, withdraw it
 * or delete it. Reads the live team, so it follows pushes while open.
 */
export function TeamDetailDialog(props: {
  event: EventInfo;
  graph: EventGraph | null | undefined;
  team: Team;
  color: string;
  /** Changes whenever the event's activity does, to re-read this team's. */
  activityKey: number;
  reload: () => void;
  onEdit: () => void;
  onClose: () => void;
}) {
  const { event, team } = props;
  const t = useT();
  const actions = useTeamActions(props.reload);
  const log = useLoad((s) => api.getActivity(event.id, team.id, s), [event.id, team.id, props.activityKey]);
  const finished = teamProgress(t, props.graph, team).done;
  const canUnlock = !finished && !!team.node && !!props.graph?.rescuable?.includes(team.node) && canDo(event, "unlock_team");
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
            onClick={async (e) => { if (await actions.unlock(team, e.altKey)) props.onClose(); }}
          >
            {t.t("Move team on")}
          </Button>
        )}
        <Button icon={<Pencil size={ICON.md} aria-hidden />} onClick={props.onEdit}>{t.t("Edit members")}</Button>
        {more.length > 0 && <MoreMenu label={t.t("More")} items={more} />}
      </div>
      {!finished && team.groups?.length ? (
        <>
          <h3 className="ev-h3">{t.t("Groups")}</h3>
          <ul className="ev-td__members">
            {team.groups.map((g) => (
              <li key={g.port}>
                <span>{g.label}</span>
                <span className="cs-sub">{stepTitle(props.graph, g.node)}</span>
                {props.graph?.rescuable?.includes(g.node) && canDo(event, "unlock_team") && (
                  <Button
                    icon={<FastForward size={ICON.sm} aria-hidden />}
                    title={t.t("Alt-click to skip the confirmation")}
                    onClick={(e) => void actions.unlock(team, e.altKey, g.node)}
                  >
                    {t.t("Move group on")}
                  </Button>
                )}
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
          {members(team).map((m) => (
            <li key={m.id}>
              <span>{m.name}</span>
              <span className="cs-sub">{m.last_login_at ? <>{t.t("Last login")} <Ago at={m.last_login_at} /></> : t.t("Not joined yet")}</span>
            </li>
          ))}
        </ul>
      )}
      <h3 className="ev-h3">{t.t("Activity")}</h3>
      {log.error ? <ErrorBox message={log.error} retry={log.reload} retryLabel={t.t("Retry")} /> : !log.data ? <Loading label={t.t("Loading…")} /> : <ActivityFeed items={log.data} event={event} graph={props.graph} />}
    </Dialog>
  );
}
