import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { get, post, request } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Table, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";

/** adminDTO, internal/admin/organizations.go. */
interface Admin {
  id: number;
  username: string;
}

/** The platform's global admins: add one, delete one (never the last — the server refuses). */
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

  return (
    <>
      <Card
        title={t.t("Admins")}
        subtitle={t.plural(admins.length, { one: t.t("{{n}} global admin"), other: t.t("{{n}} global admins") })}
        actions={
          <>
            <HelpPopover label={t.t("About admins")} body={t.t("Global admins manage organizations and other admins. The system will refuse to let you delete the last remaining admin.")} />
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
              key: "actions",
              header: <span className="cs-sr">{t.t("Actions")}</span>,
              align: "end",
              cell: (a) =>
                me?.username === a.username ? null : (
                  <Button size="sm" variant="ghost" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Delete admin {{username}}?", { username: a.username })} onClick={() => void remove(a)}>
                    {t.t("Delete")}
                  </Button>
                ),
            },
          ]}
        />
      </Card>
      {creating && <CreateAdminDialog onClose={() => setCreating(false)} onCreated={() => { setCreating(false); load.reload(); }} />}
    </>
  );
}

function CreateAdminDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const t = useT();
  const toast = useToast();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const submit = async () => {
    if (!username.trim() || !password) return;
    setBusy(true);
    setErr("");
    try {
      await post("/api/admin/admins", { username: username.trim(), password });
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
          <Button type="submit" variant="primary" loading={busy} disabled={!username.trim() || !password}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Username")}>
        <input autoFocus required autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
      </Field>
      <Field label={t.t("Password")} error={err || undefined}>
        <input type="password" required autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
    </Dialog>
  );
}
