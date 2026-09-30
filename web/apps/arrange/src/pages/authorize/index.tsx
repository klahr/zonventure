import { useState } from "react";
import { KeyRound } from "lucide-react";
import { get, post } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { Button, Card, ErrorBox, Field, Loading, Page, PageHeader, Row, useLoad } from "../../ui/index.ts";
import "./authorize.css";

/**
 * An MCP client — a claude.ai connector, say — signing its user in
 * (internal/oauth). The server sends the browser to #/authorize?… with
 * the client's request; this page names the client, and approving
 * returns the browser to it with a code.
 */

const PENDING = "zv.authorize";
/** A request older than this is stale: the client has given up on it. */
const PENDING_MS = 10 * 60 * 1000;

/**
 * Keeps an authorize request across signing in. Single sign-on comes
 * back to the console's front page, so the request would otherwise be
 * lost. Called once at load.
 */
export function rememberAuthorize() {
  try {
    if (location.hash.startsWith("#/authorize?")) sessionStorage.setItem(PENDING, JSON.stringify({ hash: location.hash, at: Date.now() }));
  } catch {
    // Storage blocked: signing in with a password still keeps the hash.
  }
}

/** Once signed in, returns to a remembered authorize request. */
export function resumeAuthorize() {
  try {
    const raw = sessionStorage.getItem(PENDING);
    if (!raw || location.hash.startsWith("#/authorize?")) return;
    sessionStorage.removeItem(PENDING);
    const p = JSON.parse(raw) as { hash: string; at: number };
    if (Date.now() - p.at < PENDING_MS) location.hash = p.hash;
  } catch {
    // Nothing to resume.
  }
}

function forget() {
  try {
    sessionStorage.removeItem(PENDING);
  } catch {
    // Nothing stored.
  }
}

interface AuthRequest {
  client_name: string;
  redirect_host: string;
  scopes: string[];
}

export function AuthorizePage() {
  const t = useT();
  const query = location.hash.replace(/^#\/authorize\??/, "");
  const load = useLoad((signal) => get<AuthRequest>("/api/admin/oauth/request?" + query, { signal }), [query]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { state } = useSession();
  const memberships = state.status === "authenticated" ? state.user.memberships : [];
  const current = state.status === "authenticated" ? state.user.org_id : undefined;
  // Someone in several organizations picks the one the assistant acts in.
  const [org, setOrg] = useState(() => current ?? memberships[0]?.org_id ?? 0);

  const answer = (approve: boolean) => {
    setBusy(true);
    setError(null);
    const q = new URLSearchParams(query);
    const body = Object.fromEntries(
      ["client_id", "redirect_uri", "response_type", "code_challenge", "code_challenge_method", "state", "scope", "resource"].map((k) => [k, q.get(k) ?? ""]),
    );
    post<{ redirect: string }>("/api/admin/oauth/approve", { ...body, approve, org_id: org }).then(
      (r) => {
        forget();
        location.replace(r.redirect);
      },
      (e: unknown) => {
        setBusy(false);
        setError(e instanceof Error ? e.message : String(e));
      },
    );
  };

  let body;
  if (load.error) body = <ErrorBox message={load.error} />;
  else if (!load.data) body = <Loading label={t.t("Loading…")} />;
  else {
    const r = load.data;
    const writes = r.scopes.includes("write");
    body = (
      <Card>
        <div className="az">
          <KeyRound size={32} aria-hidden className="az__icon" />
          <p className="az__lead">
            {writes
              ? t.t("{{client}} wants to build and run adventures as you: read and change adventures, events and teams in your organization.", { client: r.client_name })
              : t.t("{{client}} wants to read your adventures, events and teams as you. It can't change anything.", { client: r.client_name })}
          </p>
          {memberships.length > 1 && (
            <Field label={t.t("Organization")} hint={t.t("The assistant works in this organization only. Connect again to use another.")}>
              <select value={org} onChange={(e) => setOrg(Number(e.target.value))}>
                {memberships.map((m) => (
                  <option key={m.org_id} value={m.org_id}>{m.org_name}</option>
                ))}
              </select>
            </Field>
          )}
          <p className="az__muted">{t.t("After you allow it, you go back to {{host}}. You can disconnect it any time under Settings → Account → API tokens.", { host: r.redirect_host })}</p>
          {error && <ErrorBox message={error} />}
          <Row>
            <Button variant="primary" filled disabled={busy} onClick={() => answer(true)}>{t.t("Allow")}</Button>
            <Button disabled={busy} onClick={() => answer(false)}>{t.t("Deny")}</Button>
          </Row>
        </div>
      </Card>
    );
  }
  return (
    <Page>
      <PageHeader title={t.t("Connect an assistant")} />
      {body}
    </Page>
  );
}
