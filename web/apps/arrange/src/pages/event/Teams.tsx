import { useState, type FormEvent } from "react";
import { Copy, Gift, Link2, Mail, Pencil, Plus, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Card, Empty, fmtAgo, fmtDateTime, HelpPopover, ICON, Pill, useToast } from "../../ui/index.ts";
import { SPOTS } from "@zonventure/ui";
import { giveFreeTicket, type EventGraph, type EventInfo, type FreeTicket, type Team } from "./api.ts";
import { TeamAvatar, useTeamActions } from "./teamActions.tsx";
import { useInviteAll } from "./Lifecycle.tsx";
import { useCopyLinks } from "./Links.tsx";
import { MoreMenu, type MenuItem } from "./Menu.tsx";
import { canDo, copyText, isLive, isQuest, isTerminal, joinedCount, members, teamColor, teamProgress, teamStuck, teamURL } from "./util.ts";

type Filter = "all" | "done" | "stuck" | "unstarted";

/**
 * The event's teams and their links, in one place. Before the start it
 * is about getting links out and players in: who has joined, each
 * captain link, the bulk copies and the email invitations. Once it runs
 * it is about progress, filtered to the done, the stuck (joined but
 * quiet for ten minutes) and the unstarted (nobody has opened a link).
 * A team's name opens its dialog; the pencil its members and links.
 */
export function TeamsCard(props: {
  event: EventInfo;
  graph: EventGraph | null | undefined;
  teams: Team[];
  now: number;
  reload: () => void;
  onOpen: (id: number) => void;
  onEdit: (id: number) => void;
  onNew: () => void;
}) {
  const { event, teams, now } = props;
  const t = useT();
  const actions = useTeamActions(props.reload);
  const [filter, setFilter] = useState<Filter>("all");
  const [q, setQ] = useState("");
  const canCreate = canDo(event, "create_team");
  const full = event.max_players > 0 && (event.player_slots_count || 0) >= event.max_players;
  const index = new Map(teams.map((tm, i) => [tm.id, i]));
  const needle = q.trim().toLowerCase();
  const shown = teams.filter((tm) => {
    const ok =
      filter === "all" ||
      (filter === "done" && !!tm.completed_at) ||
      (filter === "stuck" && teamStuck(event, tm, now)) ||
      // Withdrawn teams are out on purpose, not unstarted.
      (filter === "unstarted" && joinedCount(tm) === 0 && !tm.withdrawn_at);
    if (!ok) return false;
    return !needle || tm.name.toLowerCase().includes(needle) || (tm.email || "").toLowerCase().includes(needle);
  });
  const chips: { key: Filter; label: string }[] = [
    { key: "all", label: t.t("All") },
    { key: "done", label: t.t("Done") },
    { key: "stuck", label: t.t("Stuck") },
    { key: "unstarted", label: t.t("Unstarted") },
  ];
  const locale = t.current();
  const toast = useToast();
  const links = useCopyLinks(teams);
  const { withEmail, invite } = useInviteAll(event, teams);
  const before = event.state === "scheduled";
  const live = isLive(event);
  const sharing = !isTerminal(event);
  const share: MenuItem[] = [
    { label: t.t("Copy captain links"), icon: <Copy size={ICON.sm} aria-hidden />, title: t.t("One line per team: name and captain link"), onClick: () => void links.captains() },
    { label: t.t("Copy all links (CSV)"), icon: <Copy size={ICON.sm} aria-hidden />, title: t.t("Captain and player links, for a spreadsheet or email merge"), onClick: () => void links.csv() },
  ];
  if (withEmail > 0 && !isQuest(event)) {
    share.push({ separator: true }, { label: t.t("Email invitations ({{n}})", { n: withEmail }), icon: <Mail size={ICON.sm} aria-hidden />, title: t.t("Email each team their join links + calendar invite"), onClick: () => void invite() });
  }
  const copyCaptain = async (tm: (typeof teams)[number]) => {
    if (await copyText(teamURL(tm))) toast.ok(t.t("Link copied"));
    else toast.err(t.t("Clipboard blocked — select and copy by hand"));
  };

  return (
    <Card
      title={t.t("Teams")}
      {...(event.max_players > 0 ? { subtitle: t.t("{{slots}} of {{max}} player seats used", { slots: event.player_slots_count || 0, max: event.max_players }) + " · " + t.plural(teams.length, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") }) } : {})}
      actions={
        <>
        {sharing && !isQuest(event) && teams.length > 0 && (
          <>
            <HelpPopover
              label={t.t("About team links")}
              body={t.t("Send each captain their team link; players pick their own name there. Links stop working seven days after the event's scheduled start (30 days after they're made when it has no date). Anyone with a link can play as that player — if one leaks, edit the team and rotate that player's link.")}
            />
            <MoreMenu label={t.t("Share links")} items={share} />
          </>
        )}
        {/* A hosted event's teams join from its player link. */}
        {!isTerminal(event) && !event.party && (
        <Button
          size="sm"
          {...(teams.length === 0 ? { variant: "primary" as const } : {})}
          icon={<Plus size={ICON.sm} aria-hidden />}
          disabled={!canCreate || full}
          title={!canCreate ? (event.state === "completed" ? t.t("Event is completed — read-only.") : t.t("Event is active.")) : full ? t.t("The event pass is full. Add player seats from More to make room.") : undefined}
          onClick={props.onNew}
        >
          {t.t("New team")}
        </Button>
        )}
        </>
      }
    >
      {canCreate && !full && <FreeTicketForm event={event} reload={props.reload} />}
      {(teams.length > 6 || live) && (
        <div className="ev-toolbar">
          {teams.length > 6 && <input type="search" className="ev-grow" placeholder={t.t("Search by name or email")} aria-label={t.t("Search by name or email")} value={q} onChange={(e) => setQ(e.target.value)} />}
          {live && <div className="ev-chips" role="group" aria-label={t.t("Filter")}>
            {chips.map((c) => (
              <Button key={c.key} size="sm" variant={filter === c.key ? "primary" : "ghost"} aria-pressed={filter === c.key} onClick={() => setFilter(c.key)}>
                {c.label}
              </Button>
            ))}
          </div>}
        </div>
      )}
      {shown.length === 0 ? (
        <Empty
          title={teams.length === 0 ? t.t("No teams yet.") : t.t("No teams match this filter.")}
          {...(teams.length === 0 ? { illustration: SPOTS.noTeams } : {})}
          {...(teams.length === 0 && canCreate ? { action: <Button variant="primary" icon={<Plus size={ICON.sm} aria-hidden />} onClick={props.onNew}>{t.t("Add the first team")}</Button> } : {})}
        />
      ) : (
        <div className="cs-table-wrap">
          <table className="cs-table ev-teams">
            <caption className="cs-sr">{t.t("Teams")}</caption>
            <thead>
              <tr>
                <th>{t.t("Team")}</th>
                <th>{before ? t.t("Players") : t.t("Progress")}</th>
                <th className="cs-wide">{t.t("Activity")}</th>
                <th><span className="cs-sr">{t.t("Actions")}</span></th>
              </tr>
            </thead>
            <tbody>
              {shown.map((tm) => {
                const progress = teamProgress(t, props.graph, tm);
                const stuck = teamStuck(event, tm, now);
                const total = members(tm).length;
                const joined = joinedCount(tm);
                return (
                  <tr key={tm.id} className={stuck ? "is-stuck" : undefined}>
                    <td>
                      <div className="ev-team">
                        <TeamAvatar team={tm} color={teamColor(index.get(tm.id) ?? 0)} size={26} />
                        <div className="ev-team__text">
                          <div className="ev-row-sm">
                            <button type="button" className="ev-team__name" onClick={() => props.onOpen(tm.id)}>{tm.name}</button>
                            {tm.completed_at && <Pill tone="good">✓ {t.t("done")}</Pill>}
                            {tm.withdrawn_at && <Pill>{t.t("withdrew")}</Pill>}
                            {tm.cheated && <Pill tone="warn">{t.t("cheated")}</Pill>}
                            {stuck && <Pill tone="warn">⚠ {t.t("stuck?")}</Pill>}
                            {!before && total > 0 && joined < total && !tm.completed_at && <Pill>{t.t("{{n}} of {{m}} joined", { n: joined, m: total })}</Pill>}
                          </div>
                          {tm.email && <a className="ev-team__email" href={"mailto:" + tm.email}>{tm.email}</a>}
                        </div>
                      </div>
                    </td>
                    <td>
                      {before ? (
                        <span className={joined === total && total > 0 ? "ev-joined is-all" : "ev-joined"}>{t.t("{{n}} of {{m}} joined", { n: joined, m: total })}</span>
                      ) : (
                        <>
                          <div className="ev-num">{progress.main}</div>
                          {progress.step && <div className="ev-muted ev-small">{progress.step}</div>}
                        </>
                      )}
                    </td>
                    <td className="cs-wide ev-muted ev-small" title={tm.last_activity_at ? fmtDateTime(tm.last_activity_at, locale) : undefined}>
                      {joined === 0 && !tm.completed_at ? t.t("Not joined yet") : tm.last_activity_at ? fmtAgo(tm.last_activity_at, t.t, locale) : "—"}
                    </td>
                    <td className="cs-end">
                      <div className="ev-row-sm ev-end">
                        {sharing && (
                          <Button variant="ghost" size="icon" icon={<Link2 size={ICON.sm} aria-hidden />} aria-label={t.t("Copy {{team}}'s captain link", { team: tm.name })} title={t.t("Copy captain link")} onClick={() => void copyCaptain(tm)} />
                        )}
                        {tm.email?.trim() && (
                          <Button variant="ghost" size="icon" icon={<Mail size={ICON.sm} aria-hidden />} aria-label={t.t("Email invitation to {{email}}", { email: tm.email })} title={t.t("Email invitation to {{email}}", { email: tm.email })} onClick={() => void actions.invite(tm)} />
                        )}
                        <Button variant="ghost" size="icon" icon={<Pencil size={ICON.sm} aria-hidden />} aria-label={t.t("Edit team")} title={t.t("Edit team")} onClick={() => props.onEdit(tm.id)} />
                        {canDo(event, "delete_team") && (
                          <Button variant="ghost" size="icon" className="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Delete team")} title={t.t("Delete team")} onClick={() => void actions.remove(tm)} />
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

/**
 * A ticket for someone who plays without paying — a tester, a guest: a
 * team of the event's ticket size, and the email a buyer gets, with the
 * team link and the voucher code. Both show here too, to hand over by
 * hand when the mail is slow.
 */
function FreeTicketForm({ event, reload }: { event: EventInfo; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [given, setGiven] = useState<(FreeTicket & { email: string }) | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const to = email.trim();
    if (!to) return;
    setBusy(true);
    try {
      const r = await giveFreeTicket(event.id, to);
      setGiven({ ...r, email: to });
      setEmail("");
      if (r.emailed) toast.ok(t.t("Free ticket sent to {{email}}", { email: to }));
      else toast.err(t.t("The team was made, but the email could not be sent. Copy the link or code below."));
      reload();
    } catch (err) {
      toast.err(err);
    } finally {
      setBusy(false);
    }
  };
  const copy = async (text: string, done: string) => {
    if (await copyText(text)) toast.ok(done);
    else toast.err(t.t("Clipboard blocked — select and copy by hand"));
  };
  return (
    <div className="ev-free">
      <form className="ev-toolbar" onSubmit={(e) => void submit(e)}>
        <input
          type="email"
          className="ev-grow"
          required
          placeholder={t.t("Email address")}
          aria-label={t.t("Email address for a free ticket")}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Button type="submit" size="sm" icon={<Gift size={ICON.sm} aria-hidden />} loading={busy} disabled={!email.trim()}>
          {t.t("Give free ticket")}
        </Button>
        <HelpPopover
          label={t.t("About free tickets")}
          body={t.t("For testers and guests who play without paying. Makes a team the size of a bought ticket and emails them the same message a buyer gets: their team link and a voucher code to type at /play.")}
        />
      </form>
      {given && (
        <p className="ev-free__given">
          {t.t("Ticket for {{email}}:", { email: given.email })} <strong className="ev-free__code">{given.voucher_code}</strong>{" "}
          <Button size="sm" variant="ghost" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copy(given.voucher_code, t.t("Code copied"))}>
            {t.t("Copy code")}
          </Button>
          <Button size="sm" variant="ghost" icon={<Link2 size={ICON.sm} aria-hidden />} onClick={() => void copy(given.team_url, t.t("Link copied"))}>
            {t.t("Copy link")}
          </Button>
        </p>
      )}
    </div>
  );
}
