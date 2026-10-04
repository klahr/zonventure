import { useRef, useState, type ReactNode } from "react";
import { Copy, KeyRound, Plus, ShieldOff } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, Dialog, Empty, ErrorBox, Field, Loading, Pill, Table, errorText, fmtDateTime, relTime, useDialogs, useLoad, useToast, type Load } from "../../ui/index.ts";
import { account, shortUA, type AuthEvent, type McpTokenCreated } from "./account-api.ts";
import { copyText } from "./account-security.tsx";

/** Loading and failure for a card's list, so each card only draws its rows. */
function Loaded<T>({ load, children }: { load: Load<T>; children: (d: T) => ReactNode }) {
  const t = useT();
  if (load.data) return <>{children(load.data)}</>;
  if (load.error) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  return <Loading label={t.t("Loading…")} />;
}

/** Rows a long list shows before "Show all". */
const FEW = 5;

/** Every device signed in to this account, this one first; sign any of them out, or all but this one. */
export function SessionsCard() {
  const t = useT();
  const toast = useToast();
  const { confirm } = useDialogs();
  const sessions = useLoad((s) => account.sessions(s), []);
  const [busy, setBusy] = useState(false);
  const [all, setAll] = useState(false);
  const loc = t.current();

  const revoke = async (id: string) => {
    try {
      await account.revokeSession(id);
      toast.ok(t.t("Session signed out"));
      sessions.reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const revokeOthers = async () => {
    const ok = await confirm({
      title: t.t("Sign out everywhere else"),
      message: t.t("Sign out of every other browser, phone, and tablet? You'll stay signed in here."),
      confirmLabel: t.t("Sign out everywhere else"),
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await account.revokeOtherSessions();
      toast.ok(t.t("Other sessions signed out"));
      sessions.reload();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  const others = (sessions.data ?? []).filter((s) => !s.current).length;

  return (
    <Card
      title={t.t("Active sessions")}
      subtitle={t.t("Every device you're signed into. If something looks unfamiliar, sign that session out.")}
      actions={
        others > 0 && (
          <Button size="sm" icon={<ShieldOff size={14} aria-hidden />} onClick={() => void revokeOthers()} loading={busy}>
            {t.t("Sign out everywhere else")}
          </Button>
        )
      }
    >
      <Loaded load={sessions}>
        {(list) =>
          list.length === 0 ? (
            <p className="acct-muted">{t.t("No active sessions.")}</p>
          ) : (
            <div className="acct-list">
              {[...list].sort((a, b) => Number(b.current) - Number(a.current)).slice(0, all ? undefined : FEW).map((s) => (
                <div key={s.id} className="acct-item">
                  <div className="acct-item__text">
                    <span className="acct-item__name">
                      {s.user_agent ? shortUA(s.user_agent) : t.t("Unknown device")}
                      {s.current && <Pill tone="good">{t.t("this device")}</Pill>}
                    </span>
                    <span className="acct-item__meta" title={s.user_agent}>
                      {[
                        s.last_used_at ? t.t("Active {{when}}", { when: relTime(s.last_used_at, loc) }) : t.t("Signed in {{when}}", { when: relTime(s.created_at, loc) }),
                        s.ip && t.t("from {{ip}}", { ip: s.ip }),
                        s.expires_at && t.t("expires {{when}}", { when: relTime(s.expires_at, loc) }),
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
                  </div>
                  {!s.current && (
                    <Button size="sm" onClick={() => void revoke(s.id)}>{t.t("Sign out")}</Button>
                  )}
                </div>
              ))}
              {list.length > FEW && (
                <div className="acct-item">
                  <Button size="sm" variant="ghost" aria-expanded={all} onClick={() => setAll((v) => !v)}>
                    {all ? t.t("Show fewer") : t.t("Show all {{n}}", { n: list.length })}
                  </Button>
                </div>
              )}
            </div>
          )
        }
      </Loaded>
    </Card>
  );
}

const EVENT: Record<string, string> = {
  login: "Sign-in",
  logout: "Sign-out",
  set_password: "Password set",
  set_username: "Username changed",
  magic_link: "Sign-in link",
  email_verify: "Email verification",
  totp_setup: "2FA setup started",
  totp_confirm: "2FA enabled",
  totp_disable: "2FA disabled",
  totp_verify: "2FA code",
  totp_recovery_code_use: "Recovery code used",
  totp_admin_reset: "2FA reset by an admin",
};

const REASON: Record<string, string> = {
  invalid_credentials: "wrong username or password",
  rate_limited: "too many attempts",
  password_not_set: "no password set",
  password_already_set: "password already set",
  expired_token: "link expired",
  unknown_token: "unknown link",
  admin_manual: "by an admin",
  totp_required: "waiting for 2FA code",
  totp_invalid: "wrong 2FA code",
  totp_challenge_expired: "2FA step expired",
  recovery_code_invalid: "wrong recovery code",
  totp_admin_reset: "reset by an admin",
};

/** The user's own recent auth log, failed sign-ins included; the latest few, the rest on request. */
export function AuditCard() {
  const t = useT();
  const events = useLoad((s) => account.audit(s), []);
  const [all, setAll] = useState(false);
  const loc = t.current();
  return (
    <Card
      title={t.t("Recent security activity")}
      subtitle={t.t("Your last 25 login, password and email-change events.")}
      flush
    >
      <Loaded load={events}>
        {(list) => (
          <>
          <Table<AuthEvent>
            caption={t.t("Recent security activity")}
            rows={all ? list : list.slice(0, FEW)}
            rowKey={(e) => e.id}
            empty={<div style={{ padding: "0 var(--dz-space-4) var(--dz-space-4)" }}><p className="acct-muted">{t.t("No recent activity.")}</p></div>}
            columns={[
              { key: "ts", header: t.t("Time"), cell: (e) => <time dateTime={e.ts} title={fmtDateTime(e.ts, loc)} className="acct-nowrap">{relTime(e.ts, loc)}</time> },
              {
                key: "event",
                header: t.t("Event"),
                cell: (e) => (
                  <div className="acct-item__text">
                    <span className="acct-item__name" title={e.event}>
                      {EVENT[e.event] ? t.t(EVENT[e.event]!) : e.event}
                      {!e.ok && <Pill tone="bad">{t.t("failed")}</Pill>}
                    </span>
                    {e.reason && e.reason !== "success" && <span className="acct-item__meta">{REASON[e.reason] ? t.t(REASON[e.reason]!) : e.reason}</span>}
                  </div>
                ),
              },
              { key: "ip", header: t.t("IP"), wide: true, cell: (e) => <span className="acct-mono">{e.ip || "—"}</span> },
            ]}
          />
          {list.length > FEW && (
            <div className="acct-more">
              <Button size="sm" variant="ghost" aria-expanded={all} onClick={() => setAll((v) => !v)}>
                {all ? t.t("Show fewer") : t.t("Show all {{n}}", { n: list.length })}
              </Button>
            </div>
          )}
          </>
        )}
      </Loaded>
    </Card>
  );
}

/**
 * Long-lived bearer tokens for scripts, agents and MCP clients. They
 * authenticate /api/admin always, and /mcp where the operator mounted
 * it (mcpEnabled). The list shows only name, prefix and use; the
 * plaintext is shown once, when the token is made.
 */
export function McpTokensCard({ mcpEnabled }: { mcpEnabled: boolean }) {
  const t = useT();
  const toast = useToast();
  const { confirm } = useDialogs();
  const tokens = useLoad((s) => account.mcpTokens(s), []);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<McpTokenCreated | null>(null);
  const endpoint = `${location.origin}/mcp`;
  const endpointRef = useRef<HTMLInputElement>(null);
  const loc = t.current();

  const revoke = async (id: number) => {
    const ok = await confirm({
      title: t.t("Revoke"),
      message: t.t("Revoke this token? Any client currently using it will stop working."),
      confirmLabel: t.t("Revoke"),
      danger: true,
    });
    if (!ok) return;
    try {
      await account.revokeMcpToken(id);
      toast.ok(t.t("Token revoked"));
      tokens.reload();
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <Card
      title={t.t("API tokens")}
      subtitle={t.t("Long-lived tokens that let scripts, agents and MCP clients act on your behalf: create adventures, run events and more. Send one as an Authorization: Bearer header to /api/admin.")}
      actions={<Button size="sm" icon={<Plus size={14} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New token")}</Button>}
    >
      {mcpEnabled && <Field label={t.t("Point your MCP client at this endpoint and paste a token below as the bearer credential:")}>
        <div className="acct-copy">
          <input ref={endpointRef} readOnly value={endpoint} onFocus={(e) => e.currentTarget.select()} />
          <Button
            size="sm"
            icon={<Copy size={14} aria-hidden />}
            title={t.t("Copy URL")}
            onClick={async () => {
              if (await copyText(endpoint, endpointRef.current)) toast.ok(t.t("URL copied"));
              else toast.err(t.t("Copy failed — select the field and copy manually."));
            }}
          >
            {t.t("Copy")}
          </Button>
        </div>
      </Field>}
      {mcpEnabled && (
        <details className="acct-guide">
          <summary>{t.t("How to connect an MCP client")}</summary>
          <ol>
            <li>{t.t("Click New token, pick its scopes and copy the token.")}</li>
            <li>{t.t("In your MCP client, add a remote (HTTP) server with the endpoint above.")}</li>
            <li>{t.t("Set the header Authorization: Bearer followed by your token.")}</li>
            <li>{t.t("Ask it to list your adventures to check it works.")}</li>
          </ol>
          <p className="acct-muted">{t.t("For example, with Claude Code:")}</p>
          <pre>{`claude mcp add --scope user --transport http zonventure ${endpoint} \
  --header "Authorization: Bearer <token>"`}</pre>
        </details>
      )}
      <Loaded load={tokens}>
        {(list) =>
          list.length === 0 ? (
            <Empty title={t.t("No API tokens yet.")} />
          ) : (
            <div className="acct-list">
              {list.map((tok) => {
                const scopes = tok.scopes ?? [];
                return (
                  <div key={tok.id} className="acct-item">
                    <div className="acct-item__text">
                      <span className="acct-item__name">
                        <KeyRound size={14} aria-hidden />
                        {tok.name}
                      </span>
                      <span className="acct-item__meta">
                        <code className="acct-mono">{tok.prefix}…</code>
                        {" · "}
                        <span title={fmtDateTime(tok.created_at, loc)}>{t.t("Created {{when}}", { when: relTime(tok.created_at, loc) })}</span>
                        {" · "}
                        {tok.last_used_at ? <span title={fmtDateTime(tok.last_used_at, loc)}>{t.t("last used {{when}}", { when: relTime(tok.last_used_at, loc) })}</span> : t.t("never used")}
                      </span>
                      <span className="acct-item__tags">
                        {/* No scopes is a fail-closed token; say so rather than hide it. */}
                        {scopes.length === 0 ? (
                          <Pill tone="warn">{t.t("no scopes")}</Pill>
                        ) : (
                          scopes.map((sc) => <Pill key={sc} tone={sc === "write" ? "accent" : "info"}>{sc === "read" ? t.t("read") : sc === "write" ? t.t("write") : sc}</Pill>)
                        )}
                      </span>
                    </div>
                    <Button size="sm" onClick={() => void revoke(tok.id)}>{t.t("Revoke")}</Button>
                  </div>
                );
              })}
            </div>
          )
        }
      </Loaded>
      {creating && (
        <CreateTokenDialog
          onClose={() => setCreating(false)}
          onCreated={(c) => {
            setCreating(false);
            setCreated(c);
            tokens.reload();
          }}
        />
      )}
      {created && <NewTokenDialog token={created.token} mcpEnabled={mcpEnabled} onClose={() => setCreated(null)} />}
    </Card>
  );
}

function CreateTokenDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (c: McpTokenCreated) => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const [read, setRead] = useState(true);
  const [write, setWrite] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const n = name.trim();
    if (!n) return setError(t.t("Name required"));
    const scopes = [...(read ? ["read"] : []), ...(write ? ["write"] : [])];
    if (scopes.length === 0) return setError(t.t("Pick at least one scope"));
    setBusy(true);
    setError(null);
    try {
      const c = await account.createMcpToken(n, scopes);
      if (!c?.token) throw new Error(t.t("The server did not return a token."));
      onCreated(c);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Create API token")}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Token name")} hint={t.t("A label for your reference. The token itself is generated.")}>
        <input autoFocus required value={name} onChange={(e) => setName(e.target.value)} placeholder={t.t('e.g. "Claude desktop"')} />
      </Field>
      <fieldset style={{ border: 0, margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 6 }}>
        <legend className="cs-field__label" style={{ marginBottom: 4 }}>{t.t("Scopes")}</legend>
        <label className="acct-check">
          <input type="checkbox" checked={read} onChange={(e) => setRead(e.target.checked)} />
          <span>{t.t("read — list, get, validate (no changes)")}</span>
        </label>
        <label className="acct-check">
          <input type="checkbox" checked={write} onChange={(e) => setWrite(e.target.checked)} />
          <span>{t.t("write — create, edit, delete, run")}</span>
        </label>
        <span className="cs-field__hint">{t.t("Scopes can only narrow what the token can do — they never grant powers beyond your account.")}</span>
      </fieldset>
      {error && <p className="acct-err" role="alert">{error}</p>}
    </Dialog>
  );
}

const CLAUDE_CODE_OS = [
  { id: "linux", label: "Linux" },
  { id: "macos", label: "macOS" },
  { id: "windows", label: "Windows" },
] as const;
type ClaudeCodeOS = (typeof CLAUDE_CODE_OS)[number]["id"];

/**
 * The `claude mcp add` line for one OS: bash/zsh continues lines with \, PowerShell with a backtick.
 * --scope user registers it for every directory, not just the one the command runs in.
 */
function claudeCodeCommand(os: ClaudeCodeOS, endpoint: string, token: string): string {
  const cont = os === "windows" ? "`" : "\\";
  return `claude mcp add --scope user --transport http zonventure ${cont}\n  ${endpoint} ${cont}\n  --header "Authorization: Bearer ${token}"`;
}

/** Ready-to-paste Claude Code commands for the new token, one per OS. */
function ClaudeCodeSetup({ token }: { token: string }) {
  const t = useT();
  const toast = useToast();
  const [os, setOs] = useState<ClaudeCodeOS>(() => (/Win/i.test(navigator.platform) ? "windows" : /Mac/i.test(navigator.platform) ? "macos" : "linux"));
  const command = claudeCodeCommand(os, `${location.origin}/mcp`, token);
  return (
    <div className="acct-setup">
      <p className="acct-muted">{t.t("Add it to Claude Code with this command:")}</p>
      <div className="acct-ostabs" role="tablist" aria-label={t.t("Operating system")}>
        {CLAUDE_CODE_OS.map((o) => (
          <button key={o.id} type="button" role="tab" aria-selected={os === o.id} className={os === o.id ? "is-on" : undefined} onClick={() => setOs(o.id)}>
            {o.label}
          </button>
        ))}
      </div>
      <div className="acct-setup__cmd" role="tabpanel">
        <pre>{command}</pre>
        <Button
          size="sm"
          icon={<Copy size={14} aria-hidden />}
          onClick={async () => {
            if (await copyText(command, null)) toast.ok(t.t("Command copied"));
            else toast.err(t.t("Copy failed — select the field and copy manually."));
          }}
        >
          {t.t("Copy command")}
        </Button>
      </div>
      <p className="acct-muted">{os === "windows" ? t.t("Run it in PowerShell.") : t.t("Run it in a terminal (bash or zsh).")}</p>
    </div>
  );
}

/** The plaintext, once. Closing takes an explicit Done: it cannot be shown again. */
function NewTokenDialog({ token, mcpEnabled, onClose }: { token: string; mcpEnabled: boolean; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const field = useRef<HTMLInputElement>(null);
  return (
    <Dialog
      title={t.t("Your new token")}
      onClose={() => {}}
      footer={
        <>
          <Button
            variant="primary"
            icon={<Copy size={14} aria-hidden />}
            onClick={async () => {
              if (await copyText(token, field.current)) toast.ok(t.t("Token copied"));
              else toast.err(t.t("Copy failed — select the field and copy manually."));
            }}
          >
            {t.t("Copy token")}
          </Button>
          <Button onClick={onClose}>{t.t("Done")}</Button>
        </>
      }
    >
      <Callout variant="warning">{t.t("Copy this token now — it will not be shown again.")}</Callout>
      <p className="acct-muted">{t.t("Send it as the bearer token from your script, agent or MCP client. Treat it like a password.")}</p>
      <div className="acct-copy">
        <input ref={field} readOnly autoFocus value={token} aria-label={t.t("Your new token")} onFocus={(e) => e.currentTarget.select()} />
      </div>
      {mcpEnabled && <ClaudeCodeSetup token={token} />}
    </Dialog>
  );
}
