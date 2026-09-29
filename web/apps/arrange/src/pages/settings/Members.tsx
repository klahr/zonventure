import { useEffect, useId, useRef, useState } from "react";
import { ArrowRightLeft, Plus, Trash2 } from "lucide-react";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Table, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { MoreMenu } from "../event/Menu.tsx";
import { addMember, getOrg, removeMember, searchUsers, transferOwnership, type OrgDetail, type OrgMember, type UserMatch } from "./members-api.ts";
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
  const roleLabel = (r: string) => (r === "org_admin" ? t.t("admin") : t.t("event host"));

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
    const pw = await dialogs.password({
      title: t.t("Remove {{username}}?", { username: m.username }),
      message: t.t("Removes {{username}} from {{org}}. Enter your password to confirm.", { username: m.username, org: org.name }),
      confirmLabel: t.t("Remove"),
      danger: true,
    });
    if (!pw) return;
    try {
      await removeMember(org.id, m.id, pw);
      toast.ok(t.t("Removed"));
      load.reload();
    } catch (e) {
      toast.err(e);
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
              body={`${t.t("Admin")}: ${t.t("Run events, manage teams, plus invite/remove users and change billing.")} ${t.t("Event host")}: ${t.t("Run events and manage teams. Cannot invite users or touch billing.")}`}
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
                <>
                  <strong>{m.username}</strong>
                  {m.id === me.id && <span className="mb-muted"> {t.t("(you)")}</span>}
                </>
              ),
            },
            { key: "role", header: t.t("Role"), cell: (m) => <Pill tone={m.role === "org_admin" ? "accent" : "neutral"}>{roleLabel(m.role)}</Pill> },
            {
              key: "actions",
              header: <span className="cs-sr">{t.t("Actions")}</span>,
              align: "end",
              cell: (m) =>
                m.id === me.id ? null : (
                  <Button size="sm" variant="ghost" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Remove {{username}}?", { username: m.username })} title={t.t("Remove")} onClick={() => void remove(m)}>
                    <span className="mb-btn-text">{t.t("Remove")}</span>
                  </Button>
                ),
            },
          ]}
        />
      </Card>
      {adding && <AddMemberDialog org={org} onClose={() => setAdding(false)} onAdded={() => { setAdding(false); load.reload(); }} />}
      {transferring && (
        <TransferDialog
          org={org}
          others={others}
          onClose={() => setTransferring(false)}
          onDone={async () => {
            setTransferring(false);
            await refresh();
            load.reload();
          }}
        />
      )}
    </>
  );
}

/**
 * Add a user. Typing an existing username (picked from the suggestions,
 * or typed exactly) adds that account as is; anything else creates a
 * new account, which needs an initial password.
 */
function AddMemberDialog({ org, onClose, onAdded }: { org: OrgDetail; onClose: () => void; onAdded: () => void }) {
  const t = useT();
  const toast = useToast();
  const listID = useId();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("org_member");
  const [picked, setPicked] = useState<UserMatch | null>(null);
  const [matches, setMatches] = useState<UserMatch[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const input = useRef<HTMLInputElement>(null);
  // The query the current `matches` answer, so blur/submit can tell a
  // stale list from a fresh one.
  const answered = useRef("");
  const pending = useRef<Promise<UserMatch[]> | null>(null);

  useEffect(() => input.current?.focus(), []);

  const lookup = (q: string): Promise<UserMatch[]> => {
    if (q.length < 2) {
      answered.current = q;
      setMatches([]);
      return Promise.resolve([]);
    }
    if (q === answered.current) return Promise.resolve(matches);
    const p = searchUsers(q, org.id)
      .then((r) => r.users ?? [])
      .catch(() => [] as UserMatch[])
      .then((users) => {
        if (input.current && input.current.value.trim() === q) {
          answered.current = q;
          setMatches(users);
        }
        return users;
      });
    pending.current = p;
    return p;
  };

  useEffect(() => {
    const q = username.trim();
    if (picked) return;
    const timer = setTimeout(() => void lookup(q), 200);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [username, picked]);

  // An exact (case-insensitive) match counts as picked, snapped to the
  // account's own spelling.
  const autoPick = async (): Promise<UserMatch | null> => {
    if (picked) return picked;
    const q = username.trim();
    if (!q) return null;
    const list = await lookup(q);
    const hit = list.find((u) => u.username.toLowerCase() === q.toLowerCase()) ?? null;
    if (hit) choose(hit);
    return hit;
  };

  const choose = (u: UserMatch) => {
    setPicked(u);
    setUsername(u.username);
    setOpen(false);
    setActive(-1);
  };

  const submit = async () => {
    const name = username.trim();
    if (!name || busy) return;
    const existing = await autoPick();
    const body: { username: string; role: string; password?: string; email?: string } = { username: existing?.username ?? name, role };
    if (!existing) {
      if (!password) {
        setErr(t.t("Set a password for the new account, or pick an existing user from the suggestions."));
        return;
      }
      body.password = password;
      if (email.trim()) body.email = email.trim();
    }
    setBusy(true);
    setErr("");
    try {
      await addMember(org.id, body);
      toast.ok(t.t("User added"));
      onAdded();
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const showList = open && !picked && matches.length > 0 && username.trim().length >= 2;
  const mode = picked
    ? { tone: "ok", text: t.t("Adding existing user — they keep their current password.") }
    : username.trim()
      ? { tone: "muted", text: t.t("Will create a new account — set an initial password below.") }
      : null;

  return (
    <Dialog
      title={t.t("Add user to {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!username.trim()}>{t.t("Add")}</Button>
        </>
      }
    >
      <div className="mb-combo">
        <Field label={t.t("Username")}>
          <input
            ref={input}
            value={username}
            required
            autoComplete="off"
            spellCheck={false}
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={showList}
            aria-controls={listID}
            aria-activedescendant={showList && active >= 0 ? `${listID}-${active}` : undefined}
            onChange={(e) => {
              setUsername(e.target.value);
              setPicked(null);
              setOpen(true);
              setActive(-1);
            }}
            onFocus={() => setOpen(true)}
            onBlur={() => {
              setTimeout(() => setOpen(false), 100);
              void autoPick();
            }}
            onKeyDown={(e) => {
              if (!showList) return;
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setActive((i) => Math.min(matches.length - 1, i + 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActive((i) => Math.max(0, i - 1));
              } else if (e.key === "Enter" && active >= 0) {
                e.preventDefault();
                choose(matches[active]!);
              } else if (e.key === "Escape") {
                e.stopPropagation();
                setOpen(false);
              }
            }}
          />
        </Field>
        {showList && (
          <ul id={listID} className="mb-suggest" role="listbox" aria-label={t.t("Username")}>
            {matches.map((u, i) => (
              <li
                key={u.id}
                id={`${listID}-${i}`}
                role="option"
                aria-selected={i === active}
                className={i === active ? "is-active" : undefined}
                onMouseDown={(e) => {
                  e.preventDefault();
                  choose(u);
                }}
                onMouseEnter={() => setActive(i)}
              >
                {u.username}
              </li>
            ))}
          </ul>
        )}
      </div>
      {mode && <p className={"mb-mode" + (mode.tone === "ok" ? " is-ok" : "")} aria-live="polite">{mode.text}</p>}
      {!picked && (
        <>
          <Field label={t.t("Email (optional)")} hint={t.t("Lets the user log in with either their username or email, and gives them a contact address for password reset.")}>
            <input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label={t.t("Password")} hint={t.t("Minimum 8 characters.")}>
            <input type="password" autoComplete="new-password" minLength={8} required={!!username.trim()} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        </>
      )}
      <Field label={t.t("Role")}>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="org_member">{t.t("Event host")}</option>
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
      toast.ok(t.t("Ownership transferred."));
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
      <p className="confirm-message">{t.t("The chosen user becomes an admin and you become a regular member. Both changes happen in one step.")}</p>
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
