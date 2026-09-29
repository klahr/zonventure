import { get, post, request, type RequestOptions } from "@zonventure/api";

/** internal/admin/me.go: sessionDTO */
export interface Session {
  id: string;
  created_at: string;
  expires_at: string;
  user_agent?: string;
  ip?: string;
  last_used_at?: string;
  current: boolean;
}

/** internal/admin/auditlog.go: auditEventDTO */
export interface AuthEvent {
  id: number;
  ts: string;
  event: string;
  ok: boolean;
  user_id?: number;
  username?: string;
  ip?: string;
  user_agent?: string;
  reason?: string;
}

/** internal/admin/mcp_tokens.go: mcpTokenDTO */
export interface McpToken {
  id: number;
  name: string;
  prefix: string;
  scopes: string[] | null;
  created_at: string;
  last_used_at?: string;
}

/** internal/admin/mcp_tokens.go: createMCPTokenResp — the plaintext is here once. */
export interface McpTokenCreated extends McpToken {
  token: string;
}

/** internal/admin/me_totp.go: totpStatusResponse */
export interface TotpStatus {
  enabled: boolean;
  enrolled_at?: string;
  server_ready: boolean;
}

/** internal/admin/me_totp.go: totpSetupResponse */
export interface TotpSetup {
  otpauth_url: string;
  secret_base32: string;
  qr_data_url: string;
}

/** internal/admin/me.go: emailChangeRevertStatus */
export interface EmailRevertStatus {
  available: boolean;
  previous_email?: string;
  expires_at?: string;
}

const pw = (password: string) => ({ headers: { "X-Confirm-Password": password } });
const del = <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: "DELETE" });

export const account = {
  sessions: (signal: AbortSignal) => get<{ sessions: Session[] | null }>("/api/admin/me/sessions", { signal }).then((r) => r?.sessions ?? []),
  revokeSession: (id: string) => del<void>(`/api/admin/me/sessions/${encodeURIComponent(id)}`),
  revokeOtherSessions: () => del<void>("/api/admin/me/sessions"),

  audit: (signal: AbortSignal) => get<{ events: AuthEvent[] | null }>("/api/admin/me/audit", { signal }).then((r) => r?.events ?? []),

  mcpTokens: (signal: AbortSignal) => get<{ tokens: McpToken[] | null }>("/api/admin/me/mcp-tokens", { signal }).then((r) => r?.tokens ?? []),
  createMcpToken: (name: string, scopes: string[]) => post<McpTokenCreated>("/api/admin/me/mcp-tokens", { name, scopes }),
  revokeMcpToken: (id: number) => del<void>(`/api/admin/me/mcp-tokens/${id}`),

  totp: (signal: AbortSignal) => get<TotpStatus>("/api/admin/me/totp", { signal }),
  totpSetup: () => post<TotpSetup>("/api/admin/me/totp/setup"),
  totpConfirm: (code: string) => post<{ recovery_codes: string[] | null }>("/api/admin/me/totp/confirm", { code }),
  totpDisable: (password: string) => post<void>("/api/admin/me/totp/disable", undefined, pw(password)),
  totpRegenerate: (password: string) => post<{ recovery_codes: string[] | null }>("/api/admin/me/totp/recovery-codes/regenerate", undefined, pw(password)),

  changePassword: (current: string, next: string) => post<void>("/api/admin/me/password", { current, new: next }),

  changeEmail: (newEmail: string) => post<unknown>("/api/admin/me/email", { new_email: newEmail }),
  emailRevertStatus: (signal: AbortSignal) => get<EmailRevertStatus>("/api/admin/me/email/revert-status", { signal }),
  revertEmail: () => post<{ email: string }>("/api/admin/me/email/revert"),
  resendVerification: () => post<unknown>("/api/auth/resend-verification"),

  /** 202 with the scheduled time; the server drops every session. */
  scheduleDeletion: (password: string) => del<unknown>("/api/admin/me", pw(password)),
  cancelDeletion: () => post<void>("/api/admin/me/cancel-deletion"),

  createOrg: (name: string) => post<unknown>("/api/admin/me/organizations", { name }),
};

/**
 * Fetch a file and hand it to the browser as a download. Through fetch
 * rather than a plain link, so a refusal shows as an error instead of a
 * downloaded HTML error page. Resolves the saved file's name.
 */
export async function download(path: string, fallbackName: string): Promise<string> {
  const res = await fetch(path, { credentials: "same-origin" });
  if (!res.ok) {
    let msg = `error ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body?.error) msg = body.error;
    } catch {
      /* not JSON: keep the status */
    }
    throw new Error(msg);
  }
  const blob = await res.blob();
  const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? fallbackName;
  saveBlob(blob, name);
  return name;
}

export function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** "macOS · Chrome" from a User-Agent, good enough for a glance. */
export function shortUA(ua: string | undefined): string {
  if (!ua) return "";
  let os = "Other";
  if (/Windows/.test(ua)) os = "Windows";
  else if (/Macintosh|Mac OS X/.test(ua)) os = "macOS";
  else if (/Android/.test(ua)) os = "Android";
  else if (/iPhone|iPad|iOS/.test(ua)) os = "iOS";
  else if (/Linux/.test(ua)) os = "Linux";
  let browser = "Browser";
  if (/Edg\//.test(ua)) browser = "Edge";
  else if (/Chrome\//.test(ua)) browser = "Chrome";
  else if (/Firefox\//.test(ua)) browser = "Firefox";
  else if (/Safari\//.test(ua)) browser = "Safari";
  return `${os} · ${browser}`;
}
