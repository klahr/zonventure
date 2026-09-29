import { useState } from "react";
import { useT } from "../../i18n.tsx";
import { useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventInfo, Team } from "./api.ts";
import { isGraphEvent, members } from "./util.ts";

/** The team's picture, or its initials on its colour; a circle, as teams are drawn everywhere. */
export function TeamAvatar({ team, color, size = 28 }: { team: Team; color: string; size?: number }) {
  const [broken, setBroken] = useState(false);
  const style = { width: size, height: size, background: color, borderColor: color, fontSize: Math.max(10, Math.round(size * 0.42)) };
  return (
    <span className="ev-avatar" style={style} aria-hidden>
      {team.avatar_url && !broken ? <img src={team.avatar_url} alt="" onError={() => setBroken(true)} /> : initials(team.name)}
    </span>
  );
}

function initials(name: string) {
  const parts = (name || "").trim().split(/\s+/).map((p) => p.replace(/[^\p{L}\p{N}]/gu, "")).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

/**
 * The team actions the page and the team dialog share, each with the
 * classic console's confirmation: deleting asks for the password, an
 * invitation says who gets what, moving a team on explains itself
 * (Alt-click skips that when triaging), withdrawing says how it shows.
 */
export function useTeamActions(event: EventInfo, reload: () => void) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const graph = isGraphEvent(event);

  const remove = async (team: Team): Promise<boolean> => {
    const password = await dialogs.password({
      title: t.t("Delete team “{{name}}”?", { name: team.name }),
      message: t.t("This removes the team and all its progress. Members stay as player accounts. Enter your password to confirm."),
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (!password) return false;
    try {
      await api.deleteTeam(team.id, password);
      toast.ok(t.t("Team deleted"));
      reload();
      return true;
    } catch (e) {
      toast.err(e);
      return false;
    }
  };

  // Existing links are re-sent verbatim — no re-mint, so a stray confirm can't sign anyone out.
  const invite = async (team: Team) => {
    const n = members(team).length;
    const ok = await dialogs.confirm({
      title: t.t("Email invitation to {{team}}?", { team: team.name }),
      message: (
        <>
          {t.t("We'll send one email to ")}<strong>{team.email}</strong>
          {t.t(" containing this team's join link(s) and a calendar invite (.ics) participants can drop into their calendar.")}{" "}
          ({n === 1 ? t.t("1 player magic-link") : t.t("{{n}} player magic-links", { n })}, {t.t("1 calendar invite (.ics)")}){" "}
          {t.t("Existing links keep working — this re-sends the same tokens, it does not invalidate any active sessions.")}
        </>
      ),
      confirmLabel: t.t("Send email"),
    });
    if (!ok) return;
    try {
      await api.sendTeamInvitation(team.id);
      toast.ok(t.t("Invitation re-sent to {{email}}", { email: team.email || "" }));
    } catch (e) {
      toast.err(e);
    }
  };

  const unlock = async (team: Team, skipConfirm: boolean): Promise<boolean> => {
    if (!skipConfirm) {
      const ok = await dialogs.confirm({
        title: graph ? t.t("Move {{name}} on?", { name: team.name }) : t.t("Skip puzzle for {{name}}?", { name: team.name }),
        message: graph
          ? t.t("This moves the team past the puzzle they're on, without points: by its skip route if it has one, otherwise as if they had answered. Useful when they're stuck or the location won't register. The action is recorded in the event log.")
          : t.t("This advances the team past their current puzzle without them submitting the answer. Useful when they're stuck. The action is recorded in the event log."),
        confirmLabel: graph ? t.t("Move team on") : t.t("Skip puzzle"),
      });
      if (!ok) return false;
    }
    try {
      // The node guards a race: the server refuses if the team has moved meanwhile.
      const r = await api.unlockTeam(team.id, graph ? team.node : undefined);
      toast.ok(r?.completed ? t.t("{{name}} finished", { name: team.name }) : t.t("{{name}} advanced", { name: team.name }));
      reload();
      return true;
    } catch (e) {
      toast.err(e);
      return false;
    }
  };

  const withdraw = async (team: Team) => {
    const ok = await dialogs.confirm({
      title: t.t("Mark “{{name}}” as withdrawn?", { name: team.name }),
      message: graph
        ? t.t("They'll show in the results as withdrawn, with the points they had. You can undo this if they return.")
        : t.t('They\'ll show in the recap as "withdrew at puzzle {{n}}". You can undo this if they return.', { n: team.page_index + 1 }),
      confirmLabel: t.t("Mark withdrawn"),
    });
    if (!ok) return;
    try {
      await api.withdrawTeam(team.id);
      toast.ok(t.t("Team marked as withdrawn"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const unwithdraw = async (team: Team) => {
    try {
      await api.unwithdrawTeam(team.id);
      toast.ok(t.t("Team is active again"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  return { remove, invite, unlock, withdraw, unwithdraw };
}
