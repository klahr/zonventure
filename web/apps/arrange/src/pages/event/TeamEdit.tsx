import { useRef, useState } from "react";
import { Copy, ExternalLink, KeyRound, Plus, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, Field, ICON, Row, useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventInfo, Member, Team } from "./api.ts";
import { Ago } from "./Activity.tsx";
import { ExpiryHint, LinkField } from "./Links.tsx";
import { TeamAvatar } from "./teamActions.tsx";
import { canDo, copyText, downscaleImage, isLive, memberURL, members, teamURL } from "./util.ts";

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
  const file = useRef<HTMLInputElement>(null);
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

  const copyAll = async () => {
    const ms = members(team);
    if (await copyText(ms.map((m) => `${m.name}: ${memberURL(m.token)}`).join("\n"))) toast.ok(t.t("Copied {{n}} links", { n: ms.length }));
    else toast.err(t.t("Clipboard blocked — select and copy by hand"));
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
        <TeamAvatar team={shown} color={color} size={56} />
        <div className="ev-stack">
          <strong>{t.t("Team photo")}</strong>
          {canAvatar ? (
            <Row gap="sm">
              <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => file.current?.click()}>{t.t("Upload photo")}</Button>
              {team.avatar_url && <Button size="sm" variant="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={() => void removeAvatar()}>{t.t("Remove photo")}</Button>}
              <input ref={file} type="file" accept="image/*" hidden aria-label={t.t("Upload photo")} onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ""; if (f) void upload(f); }} />
            </Row>
          ) : (
            <span className="ev-muted ev-small">{t.t("Read-only.")}</span>
          )}
        </div>
      </div>

      <h3 className="ev-h3">{t.t("Share with the team")}</h3>
      <p className="ev-muted ev-small ev-hint">{t.t("Send this single link to the captain. Each member taps their own name there to pick up their personal magic link.")}</p>
      <Row gap="sm">
        <LinkField url={teamURL(team)} label={t.t("Copy team share link")} />
        <a className="btn ghost icon" href={teamURL(team)} target="_blank" rel="noopener" aria-label={t.t("Open team page")} title={t.t("Open team page")}>
          <ExternalLink size={ICON.sm} aria-hidden />
        </a>
        <Button size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copyAll()}>{t.t("Copy all as text")}</Button>
      </Row>

      <h3 className="ev-h3">{t.t("Members")}</h3>
      {members(team).length === 0 ? (
        <p className="ev-muted">{t.t("No members yet. Add one below.")}</p>
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
    if (live && !(await confirm({ title: t.t("Rotate link"), message: t.t("Rotate {{slot}}'s link mid-event? The current device will be signed out and need the new link to keep playing.", { slot: slot.name }), confirmLabel: t.t("Rotate link"), danger: true }))) return;
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
      {slot.last_login_at && <span className="ev-muted ev-small ev-slot__seen">{t.t("Last login")} <Ago at={slot.last_login_at} /></span>}
    </div>
  );
}
