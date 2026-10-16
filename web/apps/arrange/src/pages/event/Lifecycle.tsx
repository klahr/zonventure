import { useEffect, useRef, useState } from "react";
import { CalendarClock, Download, Flag, Mail, MonitorPlay, Moon, Play, Terminal, Trash2 } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Dialog, Field, fmtDateTime, ICON, toLocalInput, useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventInfo, Team } from "./api.ts";
import { LiveViewDialog } from "./Links.tsx";
import { MoreMenu, type MenuItem } from "./Menu.tsx";
import { isQuest, members } from "./util.ts";

type Open = null | "start" | "no-teams" | "schedule" | "live-view";

/**
 * The event's lifecycle actions, at the top of the page: the one thing
 * the phase calls for as the primary button — start before, live control
 * during, the report after — the one everyday companion to it
 * (reschedule, pause/resume for quests), and everything else under More.
 */
export function LifecycleActions({ event, teams, reload }: { event: EventInfo; teams: Team[]; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [open, setOpen] = useState<Open>(null);
  const quest = isQuest(event);
  const live = event.state === "running" || event.state === "paused";

  const transition = async (action: "start" | "pause" | "resume") => {
    try {
      await api.transition(event.id, action);
      toast.ok(action === "start" ? t.t("Started") : action === "pause" ? t.t("Paused") : t.t("Resumed"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const pause = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Pause this event?"),
      message: t.t("Every team sees “The game is paused” on their phone. Hint countdowns stop and answers aren't accepted until you resume.") +
        (quest ? " " + t.t("Nobody can buy a ticket in the store until you resume.") : ""),
      confirmLabel: t.t("Pause"),
    });
    if (ok) await transition("pause");
  };

  // Teams an organiser can email about a change: an invited event's, with a contact email.
  const emailable = quest || event.party ? 0 : teams.filter((tm) => tm.email?.trim()).length;

  const cancel = async () => {
    const mail = { on: true };
    const text = event.public_for_sale
      ? t.t('It leaves the store for good and teams get a "cancelled" screen on their next refresh. Tickets already bought are not refunded, and buyers are not emailed. To stop selling for a while, use Stop selling instead. Enter your password to confirm.')
      : event.state === "scheduled"
      ? t.t('The event will be marked cancelled and never start. Teams already invited keep their links but get a "cancelled" screen if they open the app. Enter your password to confirm.')
      : t.t('The event ends now. Live pings stop, the leaderboard freezes, and teams get a "cancelled" screen on their next refresh. Enter your password to confirm.');
    const password = await dialogs.password({
      title: t.t("Cancel event?"),
      message: emailable > 0 ? (
        <>
          {text}
          <br />
          <label className="ev-check">
            <input type="checkbox" defaultChecked onChange={(e) => { mail.on = e.target.checked; }} />
            {t.plural(emailable, { one: t.t("Email the team that the event is cancelled"), other: t.t("Email the {{n}} teams that the event is cancelled") })}
          </label>
        </>
      ) : text,
      cancelLabel: t.t("Keep event"),
      confirmLabel: t.t("Cancel event"),
      danger: true,
    });
    if (!password) return;
    try {
      await api.cancelEvent(event.id, password, emailable > 0 && mail.on);
      toast.ok(t.t("Event cancelled"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const end = async () => {
    const password = await dialogs.password({
      title: t.t("End event?"),
      message: t.t("The event ends now for every team. Teams still playing keep the points they have, the results and recaps open, and players are signed out. Enter your password to confirm."),
      cancelLabel: t.t("Keep it running"),
      confirmLabel: t.t("End event"),
      danger: true,
    });
    if (!password) return;
    try {
      await api.endEvent(event.id, password);
      toast.ok(t.t("Event ended"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const resendHost = async () => {
    try {
      await api.resendHostLink(event.id);
      toast.ok(t.t("Host link sent to the buyer"));
    } catch (e) {
      if (e instanceof ApiError && e.code === "no_buyer") toast.err(t.t("Nobody bought this hosted event, so there is no buyer to send the link to."));
      else toast.err(e);
    }
  };

  const more: MenuItem[] = [];
  const buttons: React.ReactNode[] = [];
  const control = { name: "event-control" as const, id: event.id, quest };
  if (live) {
    buttons.push(
      <a key="live" className="btn primary" href={href(control)} title={t.t("Live leaderboard, map, and activity")}>
        <Terminal size={ICON.md} aria-hidden />
        {t.t("Live control")}
      </a>,
    );
  } else if (event.state !== "cancelled") {
    more.push({ label: t.t("Live control"), icon: <Terminal size={ICON.sm} aria-hidden />, href: href(control), title: t.t("Live leaderboard, map, and activity") });
  }
  // A hosted event starts when its first team joins from the player link.
  if (event.state === "scheduled" && !event.party) {
    buttons.push(
      <Button key="start" variant="primary" icon={<Play size={ICON.md} aria-hidden />} onClick={() => setOpen(quest || event.teams_count > 0 || teams.length > 0 ? "start" : "no-teams")}>
        {t.t("Start now")}
      </Button>,
    );
    // Quests are always on; a scheduled time would delist them.
    if (!quest) {
      buttons.push(
        <Button key="sched" icon={<CalendarClock size={ICON.md} aria-hidden />} onClick={() => setOpen("schedule")}>
          {t.t("Reschedule")}
        </Button>,
      );
    }
  }
  if (quest && event.state === "running") {
    buttons.push(
      <Button key="pause" icon={<Moon size={ICON.md} aria-hidden />} title={t.t("Pause — hides it from your store until resumed")} onClick={() => void pause()}>
        {t.t("Pause")}
      </Button>,
    );
  }
  if (quest && event.state === "paused") {
    buttons.push(
      <Button key="resume" variant="primary" icon={<Play size={ICON.md} aria-hidden />} title={t.t("Resume — lists it in your store again")} onClick={() => void transition("resume")}>
        {t.t("Resume")}
      </Button>,
    );
  }
  const cancellable = event.state === "scheduled" || live;
  if (teams.length > 0 && !quest) {
    const pdf = {
      label: t.t("Download report (PDF)"),
      icon: <Download size={ICON.sm} aria-hidden />,
      href: `/api/admin/events/${event.id}/recap.pdf`,
      download: `event-${event.id}-recap.pdf`,
      title: t.t("Download a branded PDF report you can share with stakeholders"),
    };
    // Once it is over, or every team is done, the report is what the arranger came for.
    if (event.state === "completed" || event.state === "cancelled" || teams.every((tm) => tm.completed_at)) {
      buttons.push(
        <a key="pdf" className="btn primary" href={pdf.href} download={pdf.download} title={pdf.title}>
          <Download size={ICON.md} aria-hidden />
          {pdf.label}
        </a>,
      );
    } else {
      more.push(pdf);
    }
    more.push({
      label: t.t("Download CSV"),
      icon: <Download size={ICON.sm} aria-hidden />,
      href: `/api/admin/events/${event.id}/results.csv`,
      download: `event-${event.id}-results.csv`,
      title: t.t("Download CSV of the current standings"),
    });
  }
  if (event.scheduled_at && event.state === "scheduled" && !quest) {
    more.push({
      label: t.t("Download calendar invite"),
      icon: <CalendarClock size={ICON.sm} aria-hidden />,
      href: `/api/admin/events/${event.id}/invite.ics`,
      download: `event-${event.id}-invite.ics`,
      title: t.t("Download a calendar invite (.ics) you can forward to participants"),
    });
  }
  if (event.party && cancellable) {
    more.push({ label: t.t("Resend host link to buyer"), icon: <Mail size={ICON.sm} aria-hidden />, title: t.t("Emails the buyer their host page link again"), onClick: () => void resendHost() });
  }
  if (event.state !== "cancelled" && event.live_token) {
    more.push({ label: t.t("Live view screen…"), icon: <MonitorPlay size={ICON.sm} aria-hidden />, title: t.t("Open the link on any screen — no login needed."), onClick: () => setOpen("live-view") });
  }
  if (cancellable) {
    if (more.length) more.push({ separator: true });
    // A self-guided adventure is paused or cancelled, never ended.
    if (live && !quest) more.push({ label: t.t("End event"), icon: <Flag size={ICON.sm} aria-hidden />, danger: true, onClick: () => void end() });
    more.push({ label: t.t("Cancel event"), icon: <Trash2 size={ICON.sm} aria-hidden />, danger: true, onClick: () => void cancel() });
  }

  return (
    <>
      {buttons}
      {more.length > 0 && <MoreMenu label={t.t("More")} items={more} />}
      {open === "no-teams" && (
        <Dialog title={t.t("Add at least one team first")} onClose={() => setOpen(null)} footer={<Button variant="primary" onClick={() => setOpen(null)}>{t.t("OK")}</Button>}>
          <p>{t.t("This event has no teams yet. A team is the unit that actually plays the adventure — without one, there's nothing for the first puzzle to open to.")}</p>
        </Dialog>
      )}
      {open === "start" && <StartDialog event={event} teams={teams} onClose={() => setOpen(null)} onStart={() => { setOpen(null); void transition("start"); }} />}
      {open === "schedule" && <ScheduleDialog event={event} emailable={emailable} onClose={() => setOpen(null)} onDone={reload} />}
      {open === "live-view" && <LiveViewDialog event={event} onChanged={reload} onClose={() => setOpen(null)} />}
    </>
  );
}

/** Email every team with a contact address its links and a calendar invite, after a confirm. */
export function useInviteAll(event: EventInfo, teams: Team[]) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const withEmail = teams.filter((tm) => tm.email?.trim()).length;
  const invite = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Send invitations to teams?"),
      message: (
        <>
          {withEmail === 1
            ? t.t("One team will receive an email with their player links + a calendar invite.")
            : t.t("{{n}} teams will receive an email with their player links + a calendar invite.", { n: withEmail })}{" "}
          {t.t("Each link signs in as one player, so ask teams not to share them. Teams without a contact email are skipped.")}
        </>
      ),
      confirmLabel: t.t("Send invitations"),
    });
    if (!ok) return;
    try {
      const r = await api.sendInvitations(event.id);
      const sent = r?.sent || 0, skipped = r?.skipped_no_email || 0, failed = r?.failed || 0;
      if (failed > 0) toast.err(t.t("Sent {{sent}}, failed {{failed}}, skipped {{skipped}}", { sent, failed, skipped }));
      else if (skipped > 0) toast.ok(t.t("Sent {{sent}} (skipped {{skipped}} without email)", { sent, skipped }));
      else toast.ok(t.t("Sent {{sent}} invitation(s)", { sent }));
    } catch (e) {
      toast.err(e);
    }
  };
  return { withEmail, invite };
}

function StartDialog({ event, teams, onClose, onStart }: { event: EventInfo; teams: Team[]; onClose: () => void; onStart: () => void }) {
  const t = useT();
  const cancelRef = useRef<HTMLButtonElement>(null);
  useEffect(() => cancelRef.current?.focus(), []);
  // Legal, but usually means the links haven't gone out yet.
  const nobody = !teams.some((tm) => members(tm).some((m) => m.last_login_at));
  return (
    <Dialog
      title={t.t("Start this event now?")}
      onClose={onClose}
      footer={
        <>
          <Button ref={cancelRef} onClick={onClose}>{t.t("Cancel")}</Button>
          <Button variant="primary" onClick={onStart}>{t.t("Start event")}</Button>
        </>
      }
    >
      <p>
        {t.t("Starting the event opens the first puzzle to all teams. ")}
        {event.scheduled_at ? (
          <>
            {t.t("An auto-start is currently scheduled for ")}
            <strong>{fmtDateTime(event.scheduled_at, t.current())}</strong>
            {t.t(". Starting now will replace that.")}
          </>
        ) : (
          t.t("You can pause it later if needed.")
        )}
      </p>
      {nobody && !isQuest(event) && <Callout variant="warning">{t.t("No player has opened their link yet. Make sure the links have gone out before you start.")}</Callout>}
    </Dialog>
  );
}

function ScheduleDialog({ event, emailable, onClose, onDone }: { event: EventInfo; emailable: number; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const [when, setWhen] = useState(() => toLocalInput(event.scheduled_at));
  // A new time sends the invitations again, with the updated calendar invite.
  const [mail, setMail] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const save = async (iso: string) => {
    setBusy(true);
    try {
      await api.patchEvent(event.id, { scheduled_at: iso });
      toast.ok(iso ? t.t("Schedule updated") : t.t("Auto-start cleared"));
      if (iso && mail && emailable > 0) {
        const r = await api.sendInvitations(event.id);
        if (r?.failed) toast.err(t.t("Sent {{sent}}, failed {{failed}}, skipped {{skipped}}", { sent: r.sent || 0, failed: r.failed, skipped: r.skipped_no_email || 0 }));
        else toast.ok(t.t("Sent {{sent}} invitation(s)", { sent: r?.sent || 0 }));
      }
      onClose();
      onDone();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={event.scheduled_at ? t.t("Change auto-start") : t.t("Set auto-start")}
      onClose={onClose}
      onSubmit={() => {
        if (!when) return setErr(t.t("Pick a date or use Clear"));
        const d = new Date(when);
        if (Number.isNaN(d.getTime())) return setErr(t.t("Bad date"));
        void save(d.toISOString());
      }}
      footer={
        <>
          {event.scheduled_at && <Button variant="danger" className="ev-foot-start" disabled={busy} onClick={() => void save("")}>{t.t("Clear")}</Button>}
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>
        </>
      }
    >
      <p className="cs-muted">{t.t("The server will start the event automatically at the time below. Leave empty and hit Clear to remove the auto-start.")}</p>
      <Field label={t.t("Auto-start at")} error={err}>
        <input type="datetime-local" value={when} autoFocus onChange={(e) => { setWhen(e.target.value); setErr(null); }} />
      </Field>
      {emailable > 0 && (
        <label className="ev-check">
          <input type="checkbox" checked={mail} onChange={(e) => setMail(e.target.checked)} />
          {t.plural(emailable, { one: t.t("Email the team its invitation again, with the new time"), other: t.t("Email the {{n}} teams their invitations again, with the new time") })}
        </label>
      )}
    </Dialog>
  );
}

/** Rename; empty resets to "Event #id". */
export function RenameDialog({ event, onClose, onDone }: { event: EventInfo; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const [name, setName] = useState(event.name || "");
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      title={t.t("Rename event")}
      onClose={onClose}
      onSubmit={async () => {
        setBusy(true);
        try {
          await api.patchEvent(event.id, { name: name.trim() });
          toast.ok(t.t("Renamed"));
          onClose();
          onDone();
        } catch (e) {
          toast.err(e);
        } finally {
          setBusy(false);
        }
      }}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>
        </>
      }
    >
      <Field label={t.t("Event name")} hint={t.t('Empty resets to "Event #{{id}}".', { id: event.id })}>
        <input value={name} autoFocus placeholder={t.t("Event name")} onChange={(e) => setName(e.target.value)} />
      </Field>
    </Dialog>
  );
}
