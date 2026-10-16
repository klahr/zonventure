import { useState } from "react";
import { Copy, ExternalLink, KeyRound, Plus, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, errorText, Field, ICON, ImageDrop, Row, useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventInfo, Member, Team } from "./api.ts";
import { Ago } from "./Activity.tsx";
import { ExpiryHint, LinkField, useCopyOrShow } from "./Links.tsx";
import { TeamAvatar } from "./teamActions.tsx";
import { canDo, downscaleImage, isLive, memberURL, members, teamURL } from "./util.ts";

/**
 * Edit a team: rename it, set the captain's email, change its photo,
 * share its link, and manage its players — copy, rotate or remove each
 * link, or add a player. The team is the live one from the page, so a
 * rotated link shows as soon as the page has re-read it.
 */
export function TeamEditDialog({ event, team, color, reload, onClose }: { event: EventInfo; team: Team; color: string; reload: () => void; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const canRename = canDo(event, "rename_team");
  // Pure arranger metadata, editable in every state; older backends lack the op.
  const canEmail = canDo(event, "edit_team_email") || canRename;
  const canAddSlot = canDo(event, "add_slot");
  const canAvatar = canDo(event, "set_team_avatar");
  const [name, setName] = useState(team.name);
  const [email, setEmail] = useState(team.email || "");
  const [newName, setNewName] = useState("");
  const [busy, setBusy] = useState(false);
  const [bust, setBust] = useState(0);
  const shown = { ...team, ...(team.avatar_url && bust ? { avatar_url: `${team.avatar_url}${team.avatar_url.includes("?") ? "&" : "?"}v=${bust}` } : {}) };

  const save = async () => {
    const body: { name?: string; email?: string } = {};
    const n = name.trim(), m = email.trim();
    if (n && n !== team.name) body.name = n;
    if (m !== (team.email || "")) body.email = m;
    if (Object.keys(body).length === 0) return onClose();
    setBusy(true);
    try {
      await api.patchTeam(team.id, body);
      toast.ok(t.t("Saved"));
      reload();
      onClose();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    const n = newName.trim();
    if (!n) return;
    try {
      const r = await api.addSlot(team.id, n);
      setNewName("");
      toast.ok(t.t("Added {{name}}", { name: r.name }));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const upload = async (f: File) => {
    try {
      const shrunk = await downscaleImage(f);
      await api.uploadTeamAvatar(team.id, shrunk, f.name);
      setBust(Date.now());
      toast.ok(t.t("Team photo updated"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const removeAvatar = async () => {
    try {
      await api.deleteTeamAvatar(team.id);
      toast.ok(t.t("Team photo removed"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const copyOrShow = useCopyOrShow();
  const copyAll = () => {
    const ms = members(team);
    return copyOrShow(ms.map((m) => `${m.name}: ${memberURL(m.token)}`).join("\n"), t.t("Copied {{n}} links", { n: ms.length }));
  };

  return (
    <Dialog
      title={t.t("Edit team · {{name}}", { name: team.name })}
      wide
      onClose={onClose}
      onSubmit={() => void save()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Close")}</Button>
          {(canRename || canEmail) && <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>}
        </>
      }
    >
      <Field label={t.t("Team name")}>
        <input value={name} required disabled={!canRename} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t.t("Captain email (optional)")} hint={t.t("Used by the arranger only — never shown on the team's share page.")}>
        <input type="email" value={email} placeholder="captain@example.org" autoComplete="off" disabled={!canEmail} onChange={(e) => setEmail(e.target.value)} />
      </Field>
      <div className="ev-td__head">
        {!canAvatar && <TeamAvatar team={shown} color={color} size={56} />}
        <div className="ev-stack">
          <strong>{t.t("Team photo")}</strong>
          {canAvatar ? (
            <ImageDrop
              shape="photo"
              src={shown.avatar_url || undefined}
              label={t.t("Upload photo")}
              accept="image/*"
              onFile={(f) => void upload(f)}
              onRemove={() => void removeAvatar()}
              removeLabel={t.t("Remove photo")}
            />
          ) : (
            <span className="cs-sub">{t.t("Read-only.")}</span>
          )}
        </div>
      </div>

      <h3 className="ev-h3">{t.t("Share with the team")}</h3>
      <p className="cs-sub cs-flush">{t.t("Send this single link to the captain. Each member taps their own name there to pick up their personal magic link.")}</p>
      <Row gap="sm">
        <LinkField url={teamURL(team)} label={t.t("Copy team share link")} />
        <a className="btn ghost icon" href={teamURL(team)} target="_blank" rel="noopener" aria-label={t.t("Open team page")} title={t.t("Open team page")}>
          <ExternalLink size={ICON.sm} aria-hidden />
        </a>
        <Button size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copyAll()}>{t.t("Copy all as text")}</Button>
      </Row>

      <h3 className="ev-h3">{t.t("Members")}</h3>
      {members(team).length === 0 ? (
        <p className="cs-muted">{t.t("No members yet. Add one below.")}</p>
      ) : (
        <div className="ev-slots">
          {members(team).map((m) => <SlotRow key={m.id} event={event} teamId={team.id} slot={m} reload={reload} confirm={dialogs.confirm} />)}
        </div>
      )}
      {canAddSlot && (
        <Row gap="sm">
          <input
            className="ev-grow"
            placeholder={t.t("New member name")}
            aria-label={t.t("New member name")}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void add(); } }}
          />
          <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => void add()}>{t.t("Add")}</Button>
        </Row>
      )}
    </Dialog>
  );
}

function SlotRow({ event, teamId, slot, reload, confirm }: { event: EventInfo; teamId: number; slot: Member; reload: () => void; confirm: ReturnType<typeof useDialogs>["confirm"] }) {
  const t = useT();
  const toast = useToast();
  // Mid-event, rotating or removing signs a player out for good, so ask first.
  const live = isLive(event);
  const rotate = async () => {
    const message = live
      ? t.t("Rotate {{slot}}'s link mid-event? The current device will be signed out and need the new link to keep playing.", { slot: slot.name })
      : t.t("Rotate {{slot}}'s link? The old link stops working, so send {{slot}} the new one.", { slot: slot.name });
    if (!(await confirm({ title: t.t("Rotate link"), message, confirmLabel: t.t("Rotate link"), danger: live }))) return;
    try {
      await api.rotateSlot(teamId, slot.id);
      toast.ok(t.t("Link rotated"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const remove = async () => {
    if (live && !(await confirm({ title: t.t("Remove slot"), message: t.t("Remove {{slot}} mid-event? Their link stops working immediately and any answers they've already submitted stay with the team.", { slot: slot.name }), confirmLabel: t.t("Remove"), danger: true }))) return;
    try {
      await api.removeSlot(teamId, slot.id);
      toast.ok(t.t("Removed"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };
  return (
    <div className="ev-slot">
      <strong className="ev-slot__name">{slot.name}</strong>
      <ExpiryHint expiresAt={slot.expires_at} />
      <LinkField url={memberURL(slot.token)} label={t.t("Copy link")} small />
      {canDo(event, "rotate_slot_token") && (
        <Button variant="ghost" size="icon" icon={<KeyRound size={ICON.sm} aria-hidden />} aria-label={t.t("Rotate link — invalidates the old one")} title={t.t("Rotate link — invalidates the old one")} onClick={() => void rotate()} />
      )}
      {canDo(event, "remove_slot") && (
        <Button variant="ghost" size="icon" className="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Remove slot")} title={t.t("Remove slot")} onClick={() => void remove()} />
      )}
      {slot.last_login_at && <span className="cs-sub ev-slot__seen">{t.t("Last login")} <Ago at={slot.last_login_at} /></span>}
    </div>
  );
}

/**
 * Every team's email in one table, for a class whose invitations wait on
 * them: type or paste down the column, then save once. Only the changed
 * rows are sent; a row the server refuses keeps its typed value.
 */
export function TeamEmailsDialog({ teams, reload, onClose }: { teams: Team[]; reload: () => void; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const [emails, setEmails] = useState(() => new Map(teams.map((tm) => [tm.id, tm.email || ""])));
  const [failed, setFailed] = useState(new Map<number, string>());
  const [busy, setBusy] = useState(false);
  const save = async () => {
    const changed = teams.filter((tm) => (emails.get(tm.id) ?? "").trim() !== (tm.email || ""));
    if (changed.length === 0) return onClose();
    setBusy(true);
    const errs = new Map<number, string>();
    for (const tm of changed) {
      try {
        await api.patchTeam(tm.id, { email: (emails.get(tm.id) ?? "").trim() });
      } catch (e) {
        errs.set(tm.id, errorText(e));
      }
    }
    setBusy(false);
    setFailed(errs);
    reload();
    const saved = changed.length - errs.size;
    if (errs.size === 0) {
      toast.ok(t.plural(saved, { one: t.t("Saved {{n}} email"), other: t.t("Saved {{n}} emails") }));
      onClose();
    } else toast.err(t.t("Saved {{c}}, {{f}} failed", { c: saved, f: errs.size }));
  };
  return (
    <Dialog
      title={t.t("Team emails")}
      wide
      onClose={onClose}
      onSubmit={() => void save()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>
        </>
      }
    >
      <p className="cs-muted">{t.t("Invitations go to these addresses. Used by the arranger only — never shown on the team's share page.")}</p>
      <div className="cs-table-wrap">
        <table className="cs-table">
          <thead>
            <tr>
              <th scope="col">{t.t("Team")}</th>
              <th scope="col">{t.t("Email")}</th>
            </tr>
          </thead>
          <tbody>
            {teams.map((tm) => (
              <tr key={tm.id}>
                <td>{tm.name}</td>
                <td>
                  <input
                    type="email"
                    placeholder="captain@example.org"
                    autoComplete="off"
                    aria-label={t.t("Email for {{team}}", { team: tm.name })}
                    {...(failed.has(tm.id) ? { "aria-invalid": true, title: failed.get(tm.id) } : {})}
                    value={emails.get(tm.id) ?? ""}
                    onChange={(e) => {
                      const v = e.target.value;
                      setEmails((m) => new Map(m).set(tm.id, v));
                    }}
                  />
                  {failed.has(tm.id) && <span className="cs-field__error" role="alert">{failed.get(tm.id)}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Dialog>
  );
}
