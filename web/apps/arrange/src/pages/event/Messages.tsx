import { useState } from "react";
import { Mail, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, Field, fmtDateTime, fmtWhen, ICON, MarkdownArea, Pill, useDialogs, useToast } from "../../ui/index.ts";
import { Markdown } from "@zonventure/play-ui";
import * as api from "./api.ts";
import type { EventGraph, EventInfo, Message, Photo, Team } from "./api.ts";
import { stepTitle } from "./util.ts";

type Info = Pick<EventInfo, "id" | "state">;
type Named = Pick<Team, "id" | "name">;

/**
 * What the organisers have sent the teams, newest first, each with who
 * it went to and a way to take it back. Only the graph player has an
 * inbox, so a lane event has no card; neither has an ended event that sent none.
 */
export function MessagesCard({ event, teams, messages, inbox, reload }: { event: Info; teams: Named[]; messages: Message[]; inbox: boolean; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [composing, setComposing] = useState(false);
  const names = new Map(teams.map((tm) => [tm.id, tm.name]));
  const remove = async (m: Message) => {
    const ok = await dialogs.confirm({
      title: t.t("Delete this message?"),
      message: <>“{m.text}” {t.t("Teams stop seeing it, including those who have read it.")}</>,
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMessage(event.id, m.id);
      toast.ok(t.t("Message deleted."));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const canSend = inbox && event.state !== "completed" && event.state !== "cancelled";
  if (messages.length === 0 && !canSend) return null;
  return (
    <Card
      title={t.t("Messages to teams")}
      actions={canSend && <Button size="sm" icon={<Mail size={ICON.sm} aria-hidden />} onClick={() => setComposing(true)}>{t.t("Message teams")}</Button>}
    >
      {messages.length === 0 ? (
        <p className="ev-muted">{t.t("Nothing sent yet. Messages are information beside the game: teams see them in the app’s inbox.")}</p>
      ) : (
        <ul className="ev-msgs ev-scroll">
          {messages.map((m) => {
            const to = !m.team_ids?.length ? t.t("All teams") : m.team_ids.map((id) => names.get(id) || "#" + id).join(", ");
            return (
              <li key={m.id}>
                <div className="ev-msgs__meta ev-muted">
                  <time dateTime={m.created_at} title={fmtDateTime(m.created_at, t.current())}>{fmtWhen(m.created_at, t.current())}</time> · {to}
                </div>
                <Button variant="ghost" size="icon" aria-label={t.t("Delete message")} title={t.t("Delete message")} icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={() => void remove(m)} />
                <Markdown className="ev-msgs__text" text={m.text} />
              </li>
            );
          })}
        </ul>
      )}
      {composing && <Composer event={event} teams={teams} onClose={() => setComposing(false)} onSent={() => { setComposing(false); toast.ok(t.t("Message sent.")); reload(); }} />}
    </Card>
  );
}

function Composer({ event, teams, onClose, onSent }: { event: Info; teams: Named[]; onClose: () => void; onSent: () => void }) {
  const t = useT();
  const [text, setText] = useState("");
  const [everyone, setEveryone] = useState(true);
  const [chosen, setChosen] = useState<Set<number>>(() => new Set());
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const send = async () => {
    setErr(null);
    if (!text.trim()) return setErr(t.t("Write the message first."));
    if (!everyone && chosen.size === 0) return setErr(t.t("Tick at least one team."));
    setBusy(true);
    try {
      await api.sendMessage(event.id, text, everyone ? undefined : [...chosen]);
      onSent();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Message teams")}
      onClose={onClose}
      onSubmit={() => void send()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} icon={<Mail size={ICON.sm} aria-hidden />}>{t.t("Send")}</Button>
        </>
      }
    >
      <Field label={t.t("Message")} hint={t.t("Information beside the game, such as practical news or a change of plan. It is not part of the adventure.")}>
        <MarkdownArea rows={5} maxLength={2000} autoFocus placeholder={t.t("Lunch is served at the square at 12.")} value={text} onChange={(e) => setText(e.target.value)} />
      </Field>
      {teams.length > 1 && (
        <fieldset className="ev-fieldset">
          <legend className="cs-sr">{t.t("Send to")}</legend>
          <label className="ev-check"><input type="radio" name="msg-to" checked={everyone} onChange={() => setEveryone(true)} />{t.t("All teams")}</label>
          <label className="ev-check"><input type="radio" name="msg-to" checked={!everyone} onChange={() => setEveryone(false)} />{t.t("Chosen teams")}</label>
        </fieldset>
      )}
      {!everyone && (
        <div className="ev-picks">
          {teams.map((tm) => (
            <label key={tm.id} className="ev-check">
              <input
                type="checkbox"
                checked={chosen.has(tm.id)}
                onChange={(e) => setChosen((s) => { const n = new Set(s); if (e.target.checked) n.add(tm.id); else n.delete(tm.id); return n; })}
              />
              {tm.name}
            </label>
          ))}
        </div>
      )}
      {err && <p className="cs-field__error" role="alert">{err}</p>}
    </Dialog>
  );
}

/**
 * The pictures teams took for photo challenges, those waiting first.
 * Approving adds the photo's points to the team's score; denying lets
 * the team take another. Either can be changed later. Hidden when the
 * adventure has no photo challenge and nothing was sent.
 */
export function PhotosCard({ event, graph, photos, challenges, reload }: { event: Pick<EventInfo, "id">; graph: EventGraph | null | undefined; photos: Photo[]; challenges: number; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const [big, setBig] = useState<Photo | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  if (!challenges && photos.length === 0) return null;
  const review = async (p: Photo, status: "approved" | "denied" | "pending") => {
    setBusy(p.id);
    try {
      await api.reviewPhoto(event.id, p.id, status);
      toast.ok(status === "approved" ? t.t("Photo approved: {{n}} points to {{team}}.", { n: p.worth, team: p.team_name })
        : status === "denied" ? t.t("Photo denied. {{team}} can take another.", { team: p.team_name })
          : t.t("Photo is waiting for review again."));
      reload();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(null);
    }
  };
  const waiting = photos.filter((p) => p.status === "pending").length;
  const STATUS: Record<string, ["neutral" | "good" | "bad", string]> = {
    pending: ["neutral", t.t("Waiting")],
    approved: ["good", t.t("Approved")],
    denied: ["bad", t.t("Denied")],
  };
  return (
    <Card title={t.t("Team photos")} {...(waiting ? { subtitle: t.t("{{n}} waiting for review", { n: waiting }) } : {})}>
      {photos.length === 0 ? (
        <p className="ev-muted">{t.t("No photos yet. Teams send them from the photo challenges, and you approve or deny each here.")}</p>
      ) : (
        <div className="ev-photos ev-scroll">
          {photos.map((p) => {
            const [tone, label] = STATUS[p.status] ?? STATUS.pending!;
            return (
              <div key={p.id} className="ev-photo">
                <button type="button" className="ev-photo__img" title={t.t("Show larger")} aria-label={t.t("Show the photo from {{team}} larger", { team: p.team_name })} onClick={() => setBig(p)}>
                  <img src={api.photoSrc(event.id, p.id)} alt={p.prompt || t.t("Team photo")} loading="lazy" />
                </button>
                <div className="ev-row-sm"><Pill tone={tone}>{label}</Pill><strong>{p.team_name}</strong></div>
                <div className="ev-muted ev-small">{(p.node_title || stepTitle(graph, p.node)) + " · " + t.t("{{n}} points", { n: p.worth }) + " · " + fmtWhen(p.created_at, t.current())}</div>
                {p.prompt && <div className="ev-small">{p.prompt}</div>}
                <div className="ev-row-sm">
                  {p.status !== "approved" && <Button size="sm" disabled={busy === p.id} onClick={() => void review(p, "approved")}>{t.t("Approve")}</Button>}
                  {p.status !== "denied" && <Button size="sm" variant="danger" disabled={busy === p.id} onClick={() => void review(p, "denied")}>{t.t("Deny")}</Button>}
                  {p.status !== "pending" && <Button size="sm" variant="ghost" disabled={busy === p.id} onClick={() => void review(p, "pending")}>{t.t("Undo")}</Button>}
                </div>
              </div>
            );
          })}
        </div>
      )}
      {big && (
        <Dialog title={big.team_name} wide onClose={() => setBig(null)} footer={<Button autoFocus onClick={() => setBig(null)}>{t.t("Close")}</Button>}>
          <img className="ev-photo__big" src={api.photoSrc(event.id, big.id)} alt={big.prompt || t.t("Team photo")} />
          {big.prompt && <p className="ev-muted">{big.prompt}</p>}
        </Dialog>
      )}
    </Card>
  );
}
