import { useState } from "react";
import { KeyRound, Plus, Trash2 } from "lucide-react";
import { get, post, put, request } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Table, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";

type AdminRole = "admin" | "support";

/** adminDTO, internal/admin/organizations.go. */
interface Admin {
  id: number;
  username: string;
  role: AdminRole;
  totp_enabled: boolean;
  has_email: boolean;
}

/**
 * The platform's global admins: add one as a full admin or Support, change
 * another's role, delete one (never the last — the server refuses).
 */
export function AdminsTab() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state } = useSession();
  const me = state.status === "authenticated" ? state.user : null;
  const load = useLoad((signal) => get<{ admins: Admin[] | null }>("/api/admin/admins", { signal }), []);
  const [creating, setCreating] = useState(false);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const admins = load.data.admins ?? [];

  const remove = async (a: Admin) => {
    const pw = await dialogs.password({
      title: t.t("Delete admin {{username}}?", { username: a.username }),
      message: t.t("Enter your password to confirm."),
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (!pw) return;
    try {
      await request<void>(`/api/admin/admins/${a.id}`, { method: "DELETE", headers: { "X-Confirm-Password": pw } });
      toast.ok(t.t("Admin deleted"));
      load.reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const sendResetLink = async (a: Admin) => {
    const ok = await dialogs.confirm({
      title: t.t("Send {{username}} a reset link?", { username: a.username }),
      message: t.t("They get an email with a link to choose a new password, the same one \"Forgot password\" sends. Their current password keeps working until they do."),
      confirmLabel: t.t("Send reset link"),
    });
    if (!ok) return;
    try {
      await post<void>(`/api/admin/admins/${a.id}/reset-link`);
      toast.ok(t.t("Reset link sent"));
    } catch (e) {
      toast.err(e);
    }
  };

  const changeRole = async (a: Admin, role: AdminRole) => {
    const pw = await dialogs.password({
      title: role === "support"
        ? t.t("Make {{username}} Support?", { username: a.username })
        : t.t("Make {{username}} a full admin?", { username: a.username }),
      message: t.t("Enter your password to confirm."),
      confirmLabel: t.t("Change role"),
    });
    if (!pw) return;
    try {
      await put<void>(`/api/admin/admins/${a.id}/role`, { role }, { headers: { "X-Confirm-Password": pw } });
      toast.ok(t.t("Role changed"));
      load.reload();
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <>
      <Card
        title={t.t("Admins")}
        subtitle={t.plural(admins.length, { one: t.t("{{n}} global admin"), other: t.t("{{n}} global admins") })}
        actions={
          <>
            <HelpPopover label={t.t("About admins")} body={t.t("Global admins manage organizations and other admins. Support sees the organizations, their events and the logs, and may only verify emails, resend host and team links and reset two-factor sign-in. The system will refuse to let you delete the last remaining admin.")} />
            <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New admin")}</Button>
          </>
        }
        flush
      >
        <Table
          caption={t.t("Admins")}
          rows={admins}
          rowKey={(a) => a.id}
          empty={<div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><Empty title={t.t("No admins yet.")} /></div>}
          columns={[
            {
              key: "user",
              header: t.t("Username"),
              cell: (a) => (
                <>
                  <strong>{a.username}</strong>
                  {me?.id === a.id && <span style={{ color: "var(--dz-muted)" }}> {t.t("(you)")}</span>}
                </>
              ),
            },
            {
              key: "role",
              header: t.t("Role"),
              cell: (a) =>
                me?.id === a.id ? (
                  roleLabel(t, a.role)
                ) : (
                  <select aria-label={t.t("Role of {{username}}", { username: a.username })} value={a.role} onChange={(e) => void changeRole(a, e.target.value as AdminRole)}>
                    <option value="admin">{roleLabel(t, "admin")}</option>
                    <option value="support">{roleLabel(t, "support")}</option>
                  </select>
                ),
            },
            { key: "totp", header: t.t("2FA"), cell: (a) => <Pill tone={a.totp_enabled ? "good" : "neutral"}>{a.totp_enabled ? t.t("on") : t.t("off")}</Pill> },
            {
              key: "actions",
              header: <span className="cs-sr">{t.t("Actions")}</span>,
              align: "end",
              cell: (a) =>
                me?.username === a.username ? null : (
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={<KeyRound size={ICON.sm} aria-hidden />}
                      disabled={!a.has_email}
                      title={a.has_email ? undefined : t.t("This admin has no email address.")}
                      onClick={() => void sendResetLink(a)}
                    >
                      {t.t("Send reset link")}
                    </Button>
                    <Button size="sm" variant="ghost" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Delete admin {{username}}?", { username: a.username })} onClick={() => void remove(a)}>
                      {t.t("Delete")}
                    </Button>
                  </>
                ),
            },
          ]}
        />
      </Card>
      {creating && <CreateAdminDialog onClose={() => setCreating(false)} onCreated={() => { setCreating(false); load.reload(); }} />}
    </>
  );
}

function roleLabel(t: ReturnType<typeof useT>, role: AdminRole): string {
  return role === "support" ? t.t("Support") : t.t("Admin");
}

function CreateAdminDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const t = useT();
  const toast = useToast();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<AdminRole>("admin");
  const [password, setPassword] = useState("");
  const [mine, setMine] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const submit = async () => {
    if (!username.trim() || !password || !mine) return;
    setBusy(true);
    setErr("");
    try {
      await post("/api/admin/admins", { username: username.trim(), password, role }, { headers: { "X-Confirm-Password": mine } });
      toast.ok(t.t("Admin created"));
      onCreated();
    } catch (e) {
      setErr(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("New admin")}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!username.trim() || !password || !mine}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Username")}>
        <input autoFocus required autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
      </Field>
      <Field label={t.t("Role")} hint={role === "support" ? t.t("Sees the organizations, their events and the logs; may only verify emails, resend host and team links and reset two-factor sign-in.") : t.t("Can do everything, including managing other admins.")}>
        <select value={role} onChange={(e) => setRole(e.target.value as AdminRole)}>
          <option value="admin">{roleLabel(t, "admin")}</option>
          <option value="support">{roleLabel(t, "support")}</option>
        </select>
      </Field>
      <Field label={t.t("Password")} hint={t.t("Minimum 8 characters.")}>
        <input type="password" required autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
      <Field label={t.t("Your password")} hint={t.t("Enter your password to confirm.")} error={err || undefined}>
        <input type="password" required autoComplete="current-password" value={mine} onChange={(e) => setMine(e.target.value)} />
      </Field>
    </Dialog>
  );
}
