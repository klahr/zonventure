import { useState, type FormEvent } from "react";
import { ApiError, auth } from "@zonventure/api";
import { useT } from "./i18n.tsx";

/**
 * Login, including the TOTP second step.
 *
 * The server reports `totp_required` in a 200 body rather than as an
 * error, because a first factor that succeeded is not a failure. So
 * this is a two-stage form, not two routes.
 */
export function Login({ onSuccess }: { onSuccess: () => void }) {
  const t = useT();
  const [stage, setStage] = useState<"credentials" | "totp">("credentials");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (stage === "credentials") {
        const res = await auth.login({ username, password });
        if (res?.totp_required) {
          setStage("totp");
          return;
        }
      } else {
        await auth.loginTOTP(code);
      }
      onSuccess();
    } catch (err) {
      // Branch on the stable code where the handler emits one; the
      // English message is a fallback, not a contract.
      setError(
        err instanceof ApiError
          ? t.tError(err.code, err.message)
          : t.t("Could not reach the server. Check your connection."),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login">
      <form className="login__card" onSubmit={submit}>
        <div className="login__brand">
          <span className="login__wordmark">zonventure</span>
          <span className="login__sub">STUDIO</span>
        </div>

        {stage === "credentials" ? (
          <>
            <label className="field">
              <span className="field__label">{t.t("Username or email")}</span>
              <input
                className="field__input"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoFocus
                required
              />
            </label>
            <label className="field">
              <span className="field__label">{t.t("Password")}</span>
              <input
                className="field__input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </label>
          </>
        ) : (
          <label className="field">
            <span className="field__label">
              {t.t("Authentication code")}
            </span>
            <input
              className="field__input"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              // One-time-code autocomplete lets phones offer the SMS or
              // authenticator value directly; inputMode keeps the
              // numeric keypad up on mobile.
              autoComplete="one-time-code"
              inputMode="numeric"
              pattern="[0-9]*"
              autoFocus
              required
            />
          </label>
        )}

        {error !== null && (
          // assertive: a failed login must be announced immediately,
          // not queued behind other updates.
          <p className="alert alert--danger" role="alert" aria-live="assertive">
            {error}
          </p>
        )}

        <button className="btn btn--primary" type="submit" disabled={busy}>
          {busy ? t.t("Working…") : t.t("Log in")}
        </button>
      </form>
    </main>
  );
}
