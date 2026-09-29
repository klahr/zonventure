import { useEffect, useState, type ReactNode } from "react";
import { Mail } from "lucide-react";
import { get, post, put } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, Table, errorText, useLoad } from "../../ui/index.ts";

/** settingsResponse, internal/admin/settings.go. */
interface ServerSettings {
  signup_enabled: boolean;
  landing_enabled: boolean;
  landing_pinned_by_env: boolean;
}

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
      <Card title={t.t("Installed components")} subtitle={t.t("Pluggable seams the running binary was built with. Read-only — swap an implementation by rebuilding.")} flush>
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
  const [saving, setSaving] = useState<string | null>(null);
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(null);

  const flip = async (key: "signup" | "landing", enabled: boolean) => {
    if (!load.data) return;
    const field = key === "signup" ? "signup_enabled" : "landing_enabled";
    const before = load.data;
    load.set({ ...before, [field]: enabled });
    setSaving(key);
    setStatus({ ok: true, text: t.t("Saving…") });
    try {
      const r = await put<Partial<ServerSettings>>(`/api/admin/settings/${key}`, { enabled });
      load.set({ ...before, [field]: !!r?.[field] });
      setStatus({ ok: true, text: t.t("Saved.") });
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
            label={t.t("Show public landing page")}
            help={t.t("When off, unauthenticated visitors to / are sent straight to the login screen and /pricing returns 404. Useful for self-host deployments that don't want a marketing front door.")}
            checked={s.landing_enabled}
            disabled={saving === "landing" || s.landing_pinned_by_env}
            onChange={(v) => void flip("landing", v)}
          >
            {s.landing_pinned_by_env && <p style={{ ...muted, fontStyle: "italic" }}>{t.t("Pinned by ZV_LANDING_ENABLED — unset the env var to use this toggle.")}</p>}
          </Toggle>
          {status?.text && <p style={{ ...muted, color: status.ok ? "var(--dz-muted)" : "var(--dz-danger)" }} role="status">{status.text}</p>}
        </>
      )}
    </Card>
  );
}

function Toggle(props: { label: string; help: string; checked: boolean; disabled: boolean; onChange: (v: boolean) => void; children?: ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--dz-space-1)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: "var(--dz-space-1)" }}>
        <label style={{ display: "flex", alignItems: "center", gap: "var(--dz-space-2)", fontWeight: 600, cursor: props.disabled ? "not-allowed" : "pointer" }}>
          <input type="checkbox" checked={props.checked} disabled={props.disabled} onChange={(e) => props.onChange(e.target.checked)} />
          <span>{props.label}</span>
        </label>
        <HelpPopover label={props.label} body={props.help} />
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
      await post("/api/admin/send-test-email", { to: addr });
      setStatus({ tone: "ok", text: t.t("Sent to {{to}}. Check the inbox (and spam folder).", { to: addr }) });
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
