import { useEffect, useState, type ReactNode } from "react";
import { Mail } from "lucide-react";
import { get, post, put } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Table, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";

/** settingsResponse, internal/admin/settings.go. */
interface ServerSettings {
  signup_enabled: boolean;
  admin_totp_required: boolean;
  totp_available: boolean;
}

const settingField = { signup: "signup_enabled", "admin-totp": "admin_totp_required" } as const;

/** components.Component, internal/components/components.go. */
interface Component {
  name: string;
  impl: string;
  enabled: boolean;
  notes?: string;
}

const mono = { fontFamily: "var(--dz-font-mono)", fontSize: "var(--dz-text-sm)" };
const muted = { margin: 0, color: "var(--dz-muted)", fontSize: "var(--dz-text-sm)" };

/** Server-wide switches, the components the build was made with, and an SMTP check. */
export function SystemTab() {
  const t = useT();
  const components = useLoad((signal) => get<{ components: Component[] | null }>("/api/admin/components", { signal }), []);
  const mailer = components.data?.components?.find((c) => c.name === "mailer");
  return (
    <>
      <ServerCard mailerOff={!!mailer && !mailer.enabled} />
      <MaintenanceCard />
      <Card title={t.t("Installed components")} subtitle={t.t("What this server was built and configured with, such as how it sends mail and takes payments. Read-only: change the environment file or the build to change one.")} flush>
        {components.error && !components.data ? (
          <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><ErrorBox message={components.error} retry={components.reload} retryLabel={t.t("Retry")} /></div>
        ) : !components.data ? (
          <div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><Loading label={t.t("Loading…")} /></div>
        ) : (
          <Table
            caption={t.t("Installed components")}
            rows={components.data.components ?? []}
            rowKey={(c) => c.name}
            empty={<p style={{ ...muted, padding: "0 var(--dz-space-4) var(--dz-space-4)" }}>{t.t("No components reported.")}</p>}
            columns={[
              { key: "name", header: t.t("Component"), cell: (c) => <span style={mono}>{c.name}</span> },
              { key: "impl", header: t.t("Implementation"), cell: (c) => <span style={mono}>{c.impl}</span> },
              { key: "on", header: t.t("Status"), cell: (c) => <Pill tone={c.enabled ? "good" : "neutral"}>{c.enabled ? t.t("on") : t.t("off")}</Pill> },
              { key: "notes", header: t.t("Notes"), wide: true, cell: (c) => <span style={{ color: "var(--dz-muted)", fontSize: "var(--dz-text-sm)" }}>{c.notes || ""}</span> },
            ]}
          />
        )}
      </Card>
      <TestEmailCard />
    </>
  );
}

function ServerCard({ mailerOff }: { mailerOff: boolean }) {
  const t = useT();
  const load = useLoad((signal) => get<ServerSettings>("/api/admin/settings", { signal }), []);
  const { refresh } = useSession();
  const dialogs = useDialogs();
  const [saving, setSaving] = useState<string | null>(null);
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(null);

  const flip = async (key: keyof typeof settingField, enabled: boolean) => {
    if (!load.data) return;
    const field = settingField[key];
    // Requiring 2FA of admins is confirmed (the server refuses it from an
    // admin without 2FA); dropping it needs the password.
    let headers: Record<string, string> | undefined;
    if (key === "admin-totp" && enabled) {
      const go = await dialogs.confirm({
        title: t.t("Require two-factor authentication for admins?"),
        message: t.t("Every global admin and Support user must then set up two-factor authentication: until they do, the console shows them only the setup and their API tokens are refused."),
        confirmLabel: t.t("Require it"),
        danger: true,
      });
      if (!go) return;
    }
    if (key === "admin-totp" && !enabled) {
      const pw = await dialogs.password({
        title: t.t("Stop requiring two-factor authentication for admins?"),
        message: t.t("Admins and Support may then sign in with a password alone. Enter your password to confirm."),
        confirmLabel: t.t("Stop requiring"),
        danger: true,
      });
      if (!pw) return;
      headers = { "X-Confirm-Password": pw };
    }
    const before = load.data;
    load.set({ ...before, [field]: enabled });
    setSaving(key);
    setStatus({ ok: true, text: t.t("Saving…") });
    try {
      const r = await put<Partial<ServerSettings>>(`/api/admin/settings/${key}`, { enabled }, headers ? { headers } : {});
      load.set({ ...before, [field]: !!r?.[field] });
      setStatus({ ok: true, text: t.t("Saved.") });
      // Turning 2FA on for admins may now require it of this admin too.
      if (key === "admin-totp") await refresh();
    } catch (e) {
      load.set(before);
      setStatus({ ok: false, text: errorText(e) });
    } finally {
      setSaving(null);
    }
  };

  const s = load.data;
  return (
    <Card title={t.t("Server settings")}>
      {load.error && !s ? (
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      ) : !s ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <>
          <Toggle
            label={t.t("Allow public signup")}
            help={t.t('When off, the /arrange "Sign up" link is hidden and the signup endpoint refuses new accounts. Existing arrangers can still log in. Add new arrangers via Organizations → Members.')}
            checked={s.signup_enabled}
            disabled={saving === "signup"}
            onChange={(v) => void flip("signup", v)}
          >
            {s.signup_enabled && mailerOff && (
              <Callout variant="warning">{t.t("Email isn't configured, so new accounts can't verify their address and won't be able to schedule events. Set up SMTP or turn signup off.")}</Callout>
            )}
          </Toggle>
          <Toggle
            label={t.t("Require two-factor authentication for admins")}
            help={t.t("Every global admin and Support user must set up two-factor authentication: until they do, the console shows them only the setup and their API tokens are refused. SSO sign-in counts as meeting it.")}
            checked={s.admin_totp_required}
            disabled={saving === "admin-totp" || !s.totp_available}
            onChange={(v) => void flip("admin-totp", v)}
          >
            {!s.totp_available && <p style={muted}>{t.t("Two-factor authentication is not available on this server: set ZV_TOTP_KEY first.")}</p>}
          </Toggle>
          {status?.text &&<p style={{ ...muted, color: status.ok ? "var(--dz-muted)" : "var(--dz-danger)" }} role="status">{status.text}</p>}
        </>
      )}
    </Card>
  );
}

/** MaintenanceModeStatus, internal/admin/migrate.go. */
interface Maintenance {
  on: boolean;
  pinned_by_env: boolean;
}

/** Pause every write on the instance for server work, without a migration. */
function MaintenanceCard() {
  const t = useT();
  const load = useLoad((signal) => get<Maintenance>("/api/admin/migrate/maintenance", { signal }), []);
  const dialogs = useDialogs();
  const toast = useToast();
  const { refresh } = useSession();
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");
  const flip = async (on: boolean) => {
    if (on && !(await dialogs.confirm({
      title: t.t("Turn on maintenance mode?"),
      message: t.t("Every organization stops saving and players are told the game is paused, until you turn it off here."),
      confirmLabel: t.t("Turn on"),
      danger: true,
    }))) return;
    setSaving(true);
    setErr("");
    try {
      const m = await post<Maintenance>("/api/admin/migrate/maintenance", { on });
      load.set(m);
      toast.ok(m.on ? t.t("Maintenance mode is on: saving is paused.") : t.t("Maintenance mode is off: saving works again."));
      // The console's banner follows whoami.
      void refresh();
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setSaving(false);
    }
  };
  const m = load.data;
  return (
    <Card title={t.t("Maintenance")}>
      {load.error && !m ? (
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      ) : !m ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <>
          <Toggle
            label={t.t("Maintenance mode")}
            help={t.t("Pauses every write on the instance while you work on the server: pages still load, but saving, playing on and signing up wait. Players are told the game is paused. Sign-in and this switch keep working.")}
            checked={m.on}
            disabled={saving || m.pinned_by_env}
            onChange={(v) => void flip(v)}
          >
            {m.pinned_by_env && <p style={muted}>{t.t("Pinned by ZV_MAINTENANCE_MODE in the environment file; change it there.")}</p>}
          </Toggle>
          {err && <p style={{ ...muted, color: "var(--dz-danger)" }} role="status">{err}</p>}
        </>
      )}
    </Card>
  );
}

function Toggle(props: { label: string; help: string; checked: boolean; disabled: boolean; onChange: (v: boolean) => void; children?: ReactNode }) {
  const t = useT();
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--dz-space-1)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: "var(--dz-space-1)" }}>
        <label style={{ display: "flex", alignItems: "center", gap: "var(--dz-space-2)", fontWeight: 600, cursor: props.disabled ? "not-allowed" : "pointer" }}>
          <input type="checkbox" checked={props.checked} disabled={props.disabled} onChange={(e) => props.onChange(e.target.checked)} />
          <span>{props.label}</span>
        </label>
        <HelpPopover label={t.t("About “{{name}}”", { name: props.label })} body={props.help} />
      </div>
      {props.children && <div style={{ marginLeft: "1.5rem", display: "flex", flexDirection: "column", gap: "var(--dz-space-1)" }}>{props.children}</div>}
    </div>
  );
}

function TestEmailCard() {
  const t = useT();
  const { state } = useSession();
  const [to, setTo] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<{ tone: "ok" | "err" | "muted"; text: string } | null>(null);
  const email = state.status === "authenticated" ? state.user.email ?? "" : "";
  useEffect(() => setTo((v) => v || email), [email]);

  const send = async () => {
    const addr = to.trim();
    if (!addr) return;
    setBusy(true);
    setStatus({ tone: "muted", text: t.t("Sending…") });
    try {
      const res = await post<{ status: string }>("/api/admin/send-test-email", { to: addr });
      setStatus(
        res.status === "logged"
          ? { tone: "err", text: t.t("Mail is not configured (ZV_SMTP_HOST is unset), so nothing was sent. The message, with its full text, was written to the server log instead.") }
          : { tone: "ok", text: t.t("Sent to {{to}}. Check the inbox (and spam folder).", { to: addr }) },
      );
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title={t.t("Email")} subtitle={t.t("Send a canned message to verify the SMTP configuration before signups, password resets, or invitations need to deliver.")}>
      <form className="cs-row" style={{ alignItems: "flex-end" }} onSubmit={(e) => { e.preventDefault(); void send(); }}>
        <div style={{ flex: 1, minWidth: "14rem" }}>
          <Field label={t.t("Recipient")}>
            <input type="email" required placeholder="you@example.org" value={to} onChange={(e) => setTo(e.target.value)} />
          </Field>
        </div>
        <Button type="submit" variant="primary" icon={<Mail size={ICON.md} aria-hidden />} loading={busy}>{t.t("Send test email")}</Button>
      </form>
      {status && (
        <p role="status" style={{ ...muted, color: status.tone === "ok" ? "var(--dz-success)" : status.tone === "err" ? "var(--dz-danger)" : "var(--dz-muted)" }}>{status.text}</p>
      )}
    </Card>
  );
}
