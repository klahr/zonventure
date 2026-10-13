import { useEffect, useId, useRef, useState } from "react";
import { ArrowRightLeft, Plus, Trash2 } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import { Button, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Table, errorText, fmtDate, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { MoreMenu } from "../event/Menu.tsx";
import { addMember, getOrg, removeMember, setMemberRole, setRequireTotp, transferOwnership, withdrawInvitation, type OrgDetail, type OrgInvitation, type OrgMember } from "./members-api.ts";
import { account } from "./account-api.ts";
import "./members-add.css";

/** The org's users: add or invite, remove, and hand the org over. */
export function MembersTab() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state, refresh } = useSession();
  const me = state.status === "authenticated" ? state.user : null;
  const orgID = me?.org_id ?? 0;
  const load = useLoad((signal) => getOrg(orgID, signal), [orgID]);
  const [adding, setAdding] = useState(false);
  const [transferring, setTransferring] = useState(false);

  if (!me || !orgID) return <ErrorBox message={t.t("No organization.")} />;
  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const org = load.data;
  const members = org.members ?? [];
  const others = members.filter((m) => m.id !== me.id);
  const roleLabel = (r: string) => (r === "org_admin" ? t.t("admin") : r === "org_writer" ? t.t("writer") : t.t("event host"));

  const remove = async (m: OrgMember) => {
    if (m.active_events_owned > 0) {
      const go = await dialogs.confirm({
        title: t.t("Remove member with active events?"),
        message: (
          <>
            {t.t("{{username}} created {{n}} active event(s) in {{org}}.", { username: m.username, n: m.active_events_owned, org: org.name })}
            <br />
            <br />
            {t.t("Those events will keep running but lose their original creator from the audit trail. Consider cancelling or finishing them first, or transferring ownership of the org to a different admin who can manage them after removal.")}
          </>
        ),
        confirmLabel: t.t("Remove anyway"),
        danger: true,
      });
      if (!go) return;
    }
    // A wrong password asks again rather than dropping the removal.
    let error: string | undefined;
    for (;;) {
      const pw = await dialogs.password({
        title: t.t("Remove {{username}}?", { username: m.username }),
        message: t.t("Removes {{username}} from {{org}}. Enter your password to confirm.", { username: m.username, org: org.name }),
        confirmLabel: t.t("Remove"),
        danger: true,
        ...(error ? { error } : {}),
      });
      if (!pw) return;
      try {
        await removeMember(org.id, m.id, pw);
        toast.ok(t.t("Removed"));
        load.reload();
        return;
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          error = t.t("Wrong password. Try again.");
          continue;
        }
        toast.err(e);
        return;
      }
    }
  };

  const changeRole = async (m: OrgMember, role: string) => {
    if (role === m.role) return;
    // A wrong password asks again rather than dropping the change.
    let error: string | undefined;
    for (;;) {
      const pw = await dialogs.password({
        title: t.t("Make {{username}} {{role}}?", { username: m.username, role: roleLabel(role) }),
        message: t.t("Changes what {{username}} can do in {{org}} at once. Enter your password to confirm.", { username: m.username, org: org.name }),
        confirmLabel: t.t("Change role"),
        ...(error ? { error } : {}),
      });
      if (!pw) return;
      try {
        await setMemberRole(org.id, m.id, role, pw);
        toast.ok(t.t("Role changed"));
        load.reload();
        return;
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          error = t.t("Wrong password. Try again.");
          continue;
        }
        toast.err(e);
        return;
      }
    }
  };

  return (
    <>
      <Card
        title={t.t("Members")}
        subtitle={t.plural(members.length, { one: t.t("{{n}} user in {{name}}", { name: org.name }), other: t.t("{{n}} users in {{name}}", { name: org.name }) })}
        actions={
          <>
            <HelpPopover
              label={t.t("About roles")}
              body={`${t.t("Admin")}: ${t.t("Run events, manage teams, plus invite/remove users and change billing.")} ${t.t("Event host")}: ${t.t("Run events and manage teams. Cannot invite users or touch billing.")} ${t.t("Writer")}: ${t.t("Create, edit and publish adventures. Cannot run events, invite users or touch billing.")}`}
            />
            {me.role === "org_admin" && others.length > 0 && (
              <MoreMenu label={t.t("More")} items={[{ label: t.t("Transfer ownership…"), icon: <ArrowRightLeft size={ICON.sm} aria-hidden />, onClick: () => setTransferring(true) }]} />
            )}
            <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setAdding(true)}>{t.t("Add user")}</Button>
          </>
        }
        flush
      >
        <Table
          caption={t.t("Members")}
          rows={members}
          rowKey={(m) => m.id}
          empty={<div className="mb-pad"><Empty title={t.t("No other users yet.")} /></div>}
          columns={[
            {
              key: "user",
              header: t.t("Username"),
              cell: (m) => (
                <span className="mb-user">
                  <strong>{m.username}</strong>
                  {m.id === me.id && <span className="mb-muted"> {t.t("(you)")}</span>}
                </span>
              ),
            },
            {
              key: "role",
              header: t.t("Role"),
              cell: (m) =>
                m.id === me.id ? (
                  <Pill tone={m.role === "org_admin" ? "accent" : "neutral"}>{roleLabel(m.role)}</Pill>
                ) : (
                  <select value={m.role} aria-label={t.t("Role of {{username}}", { username: m.username })} onChange={(e) => void changeRole(m, e.target.value)}>
                    <option value="org_admin">{t.t("Admin")}</option>
                    <option value="org_member">{t.t("Event host")}</option>
                    <option value="org_writer">{t.t("Writer")}</option>
                  </select>
                ),
            },
            {
              key: "totp",
              header: t.t("2FA"),
              cell: (m) => (m.totp_enabled ? <Pill tone="good">{t.t("on")}</Pill> : <Pill tone={org.require_totp ? "warn" : "neutral"}>{t.t("off")}</Pill>),
            },
            {
              key: "actions",
              header: <span className="cs-sr">{t.t("Actions")}</span>,
              align: "end",
              cell: (m) =>
                m.id === me.id ? null : (
                  <Button size="sm" variant="ghost" className="mb-remove" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Remove {{username}}?", { username: m.username })} title={t.t("Remove")} onClick={() => void remove(m)}>
                    <span className="mb-btn-text">{t.t("Remove")}</span>
                  </Button>
                ),
            },
          ]}
        />
      </Card>
      <InvitationsCard org={org} onChange={load.reload} />
      <RequireTotpCard
        org={org}
        onChange={async (on) => {
          load.set({ ...org, require_totp: on });
          // Turning it on may now require it of this admin too.
          await refresh();
        }}
      />
      {adding && <AddMemberDialog org={org} onClose={() => setAdding(false)} onAdded={() => { setAdding(false); load.reload(); }} />}
      {transferring && (
        <TransferDialog
          org={org}
          others={others}
          onClose={() => setTransferring(false)}
          onDone={async () => {
            setTransferring(false);
            // Members is an admin's tab: land where the new event host can stay.
            navigate({ name: "settings", tab: "account" });
            await refresh();
            load.reload();
          }}
        />
      )}
    </>
  );
}

/**
 * Whether every member must use two-factor authentication. Greyed out
 * when the server has no ZV_TOTP_KEY, since then nobody could set it up.
 */
function RequireTotpCard({ org, onChange }: { org: OrgDetail; onChange: (on: boolean) => Promise<void> }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const status = useLoad((s) => account.totp(s), []);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const available = !!status.data?.server_ready;
  const label = t.t("Require two-factor authentication");
  const flip = async (on: boolean) => {
    let pw: string | undefined;
    if (on) {
      const go = await dialogs.confirm({
        title: t.t("Require two-factor authentication?"),
        message: t.t("Every member must then set up two-factor authentication: until they do, the console shows them only the setup and their API tokens are refused."),
        confirmLabel: t.t("Require it"),
        danger: true,
      });
      if (!go) return;
    }
    // A wrong password asks again rather than dropping the change.
    let error: string | undefined;
    for (;;) {
      if (!on) {
        const p = await dialogs.password({
          title: t.t("Stop requiring two-factor authentication?"),
          message: t.t("Members may then sign in with a password alone. Enter your password to confirm."),
          confirmLabel: t.t("Stop requiring"),
          danger: true,
          ...(error ? { error } : {}),
        });
        if (!p) return;
        pw = p;
      }
      setBusy(true);
      setErr("");
      try {
        const r = await setRequireTotp(org.id, on, pw);
        await onChange(!!r?.require_totp);
        toast.ok(r?.require_totp ? t.t("Two-factor authentication is now required.") : t.t("Two-factor authentication is no longer required."));
        return;
      } catch (e) {
        if (!on && e instanceof ApiError && e.status === 401) {
          error = t.t("Wrong password. Try again.");
          continue;
        }
        setErr(errorText(e));
        return;
      } finally {
        setBusy(false);
      }
    }
  };
  return (
    <Card title={t.t("Two-factor authentication")}>
      <div className="cs-row cs-row--sm">
        <label className="mb-check">
          <input type="checkbox" checked={org.require_totp} disabled={busy || !available} onChange={(e) => void flip(e.target.checked)} />
          <span>{label}</span>
        </label>
        <HelpPopover label={t.t("About “{{name}}”", { name: label })} body={t.t("Every member must set up two-factor authentication: until they do, the console shows them only the setup and their API tokens are refused. SSO sign-in counts as meeting it. The 2FA column shows who has it on.")} />
      </div>
      {status.data && !available && <p className="mb-mode">{t.t("Two-factor authentication is not available on this server: set ZV_TOTP_KEY first.")}</p>}
      {err && <p className="mb-err" role="status">{err}</p>}
    </Card>
  );
}

/** Invitations no one has answered yet; withdrawing one makes it stop working. */
function InvitationsCard({ org, onChange }: { org: OrgDetail; onChange: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const invitations = org.invitations ?? [];
  if (invitations.length === 0) return null;
  const roleLabel = (r: string) => (r === "org_admin" ? t.t("admin") : r === "org_writer" ? t.t("writer") : t.t("event host"));
  const withdraw = async (inv: OrgInvitation) => {
    const go = await dialogs.confirm({
      title: t.t("Withdraw the invitation to {{email}}?", { email: inv.email }),
      message: t.t("The invitation stops working. You can invite them again later."),
      confirmLabel: t.t("Withdraw"),
      danger: true,
    });
    if (!go) return;
    try {
      await withdrawInvitation(org.id, inv.user_id);
      toast.ok(t.t("Invitation withdrawn"));
      onChange();
    } catch (e) {
      toast.err(e);
    }
  };
  return (
    <Card title={t.t("Pending invitations")} subtitle={t.t("They join once they accept.")} flush>
      <Table
        caption={t.t("Pending invitations")}
        rows={invitations}
        rowKey={(i) => i.user_id}
        columns={[
          { key: "email", header: t.t("Email"), cell: (i) => <span className="mb-user">{i.email}{i.declined && <> <Pill tone="warn">{t.t("Declined")}</Pill></>}</span> },
          { key: "role", header: t.t("Role"), cell: (i) => <Pill tone={i.role === "org_admin" ? "accent" : "neutral"}>{roleLabel(i.role)}</Pill> },
          { key: "sent", header: t.t("Sent"), cell: (i) => fmtDate(i.created_at, t.current()) },
          {
            key: "actions",
            header: <span className="cs-sr">{t.t("Actions")}</span>,
            align: "end",
            cell: (i) => (
              <Button size="sm" variant="ghost" className="mb-remove" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Withdraw the invitation to {{email}}?", { email: i.email })} title={t.t("Withdraw")} onClick={() => void withdraw(i)}>
                <span className="mb-btn-text">{t.t("Withdraw")}</span>
              </Button>
            ),
          },
        ]}
      />
    </Card>
  );
}

/**
 * Add a user. An email address that has an account invites that account
 * (it joins once its owner accepts, and keeps its password); otherwise a
 * new account is created with the username, invited the same way, and
 * its owner chooses the password from the invitation email. The
 * dialog never says which, and nor does the server: that would tell an
 * org admin which addresses on the instance have accounts.
 */
function AddMemberDialog({ org, onClose, onAdded }: { org: OrgDetail; onClose: () => void; onAdded: () => void }) {
  const t = useT();
  const toast = useToast();
  const emailHint = useId();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  // The first admin is the likely first user; after that, least privilege.
  const [role, setRole] = useState((org.members ?? []).some((m) => m.role === "org_admin") ? "org_member" : "org_admin");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => input.current?.focus(), []);

  const submit = async () => {
    const name = username.trim();
    const to = email.trim();
    if (!name || !to || busy) return;
    setBusy(true);
    setErr("");
    try {
      await addMember(org.id, { username: name, role, email: to });
      toast.ok(t.t("Invitation sent to {{email}}.", { email: to }));
      onAdded();
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("Add user to {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!username.trim() || !email.trim()}>{t.t("Send invitation")}</Button>
        </>
      }
    >
      <Field label={t.t("Email")} hint={<span id={emailHint}>{t.t("They get an invitation by email. Someone new chooses their own password from it; an existing account accepts when they next sign in.")}</span>}>
        <input ref={input} type="email" required autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} aria-label={t.t("Email")} aria-describedby={emailHint} />
      </Field>
      <Field label={t.t("Username")} hint={t.t("For someone new: the name they sign in with.")}>
        <input value={username} required autoComplete="off" spellCheck={false} onChange={(e) => setUsername(e.target.value)} />
      </Field>
      <Field label={t.t("Role")}>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="org_member">{t.t("Event host")}</option>
          <option value="org_writer">{t.t("Writer")}</option>
          <option value="org_admin">{t.t("Admin")}</option>
        </select>
      </Field>
      {err && <p className="mb-err" role="alert">{err}</p>}
    </Dialog>
  );
}

function TransferDialog({ org, others, onClose, onDone }: { org: OrgDetail; others: OrgMember[]; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [to, setTo] = useState(String(others[0]?.id ?? ""));
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const id = Number.parseInt(to, 10);
    if (!Number.isFinite(id)) return;
    const pw = await dialogs.password({
      title: t.t("Confirm transfer"),
      message: t.t("Type your password to confirm. You'll lose admin permissions immediately."),
      confirmLabel: t.t("Transfer ownership"),
      danger: true,
    });
    if (!pw) return;
    setBusy(true);
    try {
      await transferOwnership(org.id, id, pw);
      toast.ok(t.t("Ownership transferred. You are now an event host in {{name}}.", { name: org.name }));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("Transfer ownership of {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Transfer ownership")}</Button>
        </>
      }
    >
      <p className="confirm-message">{t.t("The chosen user becomes an admin and you become an event host. Both changes happen in one step.")}</p>
      <Field label={t.t("Promote to admin")}>
        <select value={to} required onChange={(e) => setTo(e.target.value)}>
          {others.map((m) => (
            <option key={m.id} value={m.id}>{m.username + (m.role === "org_admin" ? ` (${t.t("admin")})` : "")}</option>
          ))}
        </select>
      </Field>
    </Dialog>
  );
}
