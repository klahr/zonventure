import { useEffect, useMemo, useState } from "react";
import { Eye, Mail, Send } from "lucide-react";
import { emails } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, EmailPreview, Field, fmtDateTime, fmtWhen, ICON, MarkdownArea, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import type { EventInfo, Team } from "./api.ts";
import { languageName } from "../../languages.ts";

type Info = Pick<EventInfo, "id" | "state" | "name" | "languages">;
type Named = Pick<Team, "id" | "name" | "email">;

/**
 * Email to the event's teams: what was sent, the theme the event's
 * emails go out in — the organisation's, or one of its own — and a
 * composer with a live preview, filled in as the first team will get it.
 * Every email the event sends uses the theme: invitations, emails from
 * steps during the game, and these.
 */
export function EmailsCard({ event, teams, from, refresh }: { event: Info; teams: Named[]; from: string; refresh: number }) {
  const t = useT();
  const toast = useToast();
  // Re-read with the event's activity: teams added, an address saved, invitations sent.
  const state = useLoad((s) => emails.eventEmails(event.id, s), [event.id, refresh]);
  const catalog = useLoad(() => emails.themes(), []);
  const [composing, setComposing] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const data = state.data;
  const themeName = (id: string) => {
    const name = catalog.data?.themes.find((x) => x.id === id)?.name;
    return name ? t.t(name) : id;
  };
  const setTheme = async (theme: emails.Theme | "") => {
    try {
      await emails.setEventTheme(event.id, theme);
      state.reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const ended = event.state === "completed" || event.state === "cancelled";
  return (
    <Card
      title={t.t("Emails to teams")}
      actions={
        <>
          <Button size="sm" variant="ghost" icon={<Eye size={ICON.sm} aria-hidden />} onClick={() => setPreviewing(true)}>
            {t.t("Preview emails")}
          </Button>
          {!ended && (
            <Button size="sm" icon={<Mail size={ICON.sm} aria-hidden />} disabled={!data?.with_email} title={data && !data.with_email ? t.t("No team has a contact email yet.") : undefined} onClick={() => setComposing(true)}>
              {t.t("Email teams")}
            </Button>
          )}
        </>
      }
    >
      {data && (
        <>
          <div className="ev-mailtheme">
            <Field label={t.t("Theme of this event's emails")}>
              <select value={data.theme} onChange={(e) => void setTheme(e.target.value as emails.Theme | "")}>
                <option value="">{data.theme ? t.t("The organisation's") : t.t("The organisation's ({{theme}})", { theme: themeName(data.theme_used) })}</option>
                {catalog.data?.themes.map((x) => (
                  <option key={x.id} value={x.id}>
                    {t.t(x.name)}
                  </option>
                ))}
              </select>
            </Field>
            <p className="cs-muted">
              {t.t("{{n}} of {{total}} teams have a contact email.", { n: data.with_email, total: data.teams_total })}{" "}
              {t.t("Invitations and emails from steps use this theme too.")}
            </p>
          </div>
          {data.emails.length === 0 ? (
            <p className="cs-muted">{t.t("Nothing emailed yet. Emails reach each team's contact address, outside the app.")}</p>
          ) : (
            <ul className="ev-mails ev-scroll">
              {data.emails.map((m) => (
                <li key={m.id}>
                  <div className="ev-mails__subject">{m.subject}</div>
                  <div className="cs-muted">
                    <time dateTime={m.sent_at} title={fmtDateTime(m.sent_at, t.current())}>
                      {fmtWhen(m.sent_at, t.current())}
                    </time>
                    {m.sent_by && ` · ${m.sent_by}`} · {t.t(m.sent === 1 ? "1 team" : "{{n}} teams", { n: m.sent })}
                    {m.skipped > 0 && ` · ${t.t("{{n}} without an address", { n: m.skipped })}`}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {composing && (
        <EmailComposer
          event={event}
          teams={teams}
          from={from}
          onClose={() => setComposing(false)}
          onSent={(sent, skipped) => {
            setComposing(false);
            toast.ok(
              skipped
                ? t.t("Sent to {{n}} teams. {{skipped}} had no contact email.", { n: sent, skipped })
                : t.t(sent === 1 ? "Sent to 1 team." : "Sent to {{n}} teams.", { n: sent }),
            );
            state.reload();
          }}
        />
      )}
      {previewing && <PreviewDialog event={event} from={from} kinds={(catalog.data?.kinds ?? []).filter((k) => k.audience === "players")} onClose={() => setPreviewing(false)} />}
    </Card>
  );
}

/** Wait a moment after the last change before rendering, so typing stays smooth. */
function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

function EmailComposer({
  event,
  teams,
  from,
  onClose,
  onSent,
}: {
  event: Info;
  teams: Named[];
  from: string;
  onClose: () => void;
  onSent: (sent: number, skipped: number) => void;
}) {
  const t = useT();
  const dialogs = useDialogs();
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  // The email in the adventure's other languages: a team playing in one
  // gets it in that, the email around it too; every other as written.
  const others = (event.languages ?? []).slice(1);
  const [translations, setTranslations] = useState<Record<string, { subject: string; body: string }>>({});
  const translate = (lang: string, field: "subject" | "body", value: string) =>
    setTranslations((m) => ({ ...m, [lang]: { subject: "", body: "", ...m[lang], [field]: value } }));
  const [everyone, setEveryone] = useState(true);
  const [chosen, setChosen] = useState<Set<number>>(() => new Set());
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<emails.Preview | null>(null);
  const [loading, setLoading] = useState(false);

  const draft = useDebounced(useMemo(() => ({ subject, body }), [subject, body]), 300);
  useEffect(() => {
    const ac = new AbortController();
    setLoading(true);
    emails
      .previewDraft({ subject: draft.subject || t.t("Your subject"), body: draft.body || t.t("Your message to {{team}}."), event_id: event.id }, ac.signal)
      .then(setPreview)
      .catch(() => {
        /* keep the last preview */
      })
      .finally(() => !ac.signal.aborted && setLoading(false));
    return () => ac.abort();
  }, [draft, event.id, t]);

  const reachable = teams.filter((tm) => tm.email?.trim());
  const to = everyone ? reachable : reachable.filter((tm) => chosen.has(tm.id));
  const send = async () => {
    setErr(null);
    if (!subject.trim()) return setErr(t.t("Write a subject."));
    if (!body.trim()) return setErr(t.t("Write the message first."));
    if (!everyone && chosen.size === 0) return setErr(t.t("Tick at least one team."));
    const ok = await dialogs.confirm({
      title: t.t(to.length === 1 ? "Email 1 team?" : "Email {{n}} teams?", { n: to.length }),
      message: t.t("Each team's contact address gets “{{subject}}”. An email can't be taken back.", { subject: subject.trim() }),
      confirmLabel: t.t("Send"),
    });
    if (!ok) return;
    setBusy(true);
    try {
      const r = await emails.emailTeams(event.id, { subject, body, translations, ...(everyone ? {} : { team_ids: [...chosen] }) });
      onSent(r.sent, r.skipped);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      wide
      title={t.t("Email teams")}
      onClose={onClose}
      onSubmit={() => void send()}
      footer={
        <>
          <span className="cs-muted ev-mail__count">{t.t(to.length === 1 ? "To 1 team" : "To {{n}} teams", { n: to.length })}</span>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={to.length === 0} icon={<Send size={ICON.sm} aria-hidden />}>
            {t.t("Send")}
          </Button>
        </>
      }
    >
      <div className="ev-mail">
        <div className="ev-mail__form">
          <Field label={t.t("Subject")}>
            <input type="text" maxLength={200} autoFocus value={subject} placeholder={t.t("Meet at the lighthouse at 14:00")} onChange={(e) => setSubject(e.target.value)} />
          </Field>
          <Field label={t.t("Message")} hint={t.t("{{team}} and {{event}} become each team's name and the event's. **Bold**, *italics* and links work; line breaks stay.", { team: "{{team}}", event: "{{event}}" })}>
            <MarkdownArea rows={9} maxLength={20000} value={body} placeholder={t.t("Hi {{team}}! …", { team: "{{team}}" })} onChange={(e) => setBody(e.target.value)} />
          </Field>
          {others.map((l) => (
            <fieldset key={l} className="ev-fieldset">
              <legend>{t.t("In {{lang}} (optional)", { lang: languageName(l) })}</legend>
              <Field label={t.t("Subject")}>
                <input type="text" maxLength={200} value={translations[l]?.subject ?? ""} onChange={(e) => translate(l, "subject", e.target.value)} />
              </Field>
              <Field label={t.t("Message")} hint={t.t("Teams playing in it get this instead, with the email around it in their language.")}>
                <MarkdownArea rows={5} maxLength={20000} value={translations[l]?.body ?? ""} onChange={(e) => translate(l, "body", e.target.value)} />
              </Field>
            </fieldset>
          ))}
          {teams.length > 1 && (
            <fieldset className="ev-fieldset">
              <legend className="cs-sr">{t.t("Send to")}</legend>
              <label className="ev-check">
                <input type="radio" name="mail-to" checked={everyone} onChange={() => setEveryone(true)} />
                {t.t("All teams")}
              </label>
              <label className="ev-check">
                <input type="radio" name="mail-to" checked={!everyone} onChange={() => setEveryone(false)} />
                {t.t("Chosen teams")}
              </label>
            </fieldset>
          )}
          {!everyone && (
            <div className="ev-picks">
              {teams.map((tm) => (
                <label key={tm.id} className={"ev-check" + (tm.email?.trim() ? "" : " is-off")} title={tm.email?.trim() ? tm.email : t.t("No contact email")}>
                  <input
                    type="checkbox"
                    disabled={!tm.email?.trim()}
                    checked={chosen.has(tm.id)}
                    onChange={(e) =>
                      setChosen((s) => {
                        const n = new Set(s);
                        if (e.target.checked) n.add(tm.id);
                        else n.delete(tm.id);
                        return n;
                      })
                    }
                  />
                  {tm.name}
                </label>
              ))}
            </div>
          )}
          {reachable.length < teams.length && (
            <p className="cs-muted">{t.t("Teams without a contact email are left out. Add one in a team's settings.")}</p>
          )}
          {err && (
            <p className="cs-field__error" role="alert">
              {err}
            </p>
          )}
        </div>
        <EmailPreview
          preview={preview}
          loading={loading}
          from={from}
          onSendTest={() => emails.sendMeTest({ subject: subject || t.t("Your subject"), body: body || t.t("Your message to {{team}}."), event_id: event.id })}
        />
      </div>
    </Dialog>
  );
}

/** Every email the event sends teams, as they will get it, in its theme. */
function PreviewDialog({ event, from, kinds, onClose }: { event: Info; from: string; kinds: emails.Kind[]; onClose: () => void }) {
  const t = useT();
  const [kind, setKind] = useState("team_invitation");
  const preview = useLoad((s) => emails.preview(kind, { eventId: event.id, signal: s }), [kind, event.id]);
  return (
    <Dialog wide title={t.t("Preview emails")} onClose={onClose} footer={<Button onClick={onClose}>{t.t("Close")}</Button>}>
      <Field label={t.t("Email")}>
        <select value={kind} onChange={(e) => setKind(e.target.value)}>
          {kinds.map((k) => (
            <option key={k.id} value={k.id}>
              {t.t(k.name)}
            </option>
          ))}
        </select>
      </Field>
      <EmailPreview preview={preview.data ?? null} loading={preview.loading} from={from} onSendTest={() => emails.sendMeTest({ sample: kind, event_id: event.id })} />
    </Dialog>
  );
}
