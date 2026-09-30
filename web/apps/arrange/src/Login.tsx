import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { ApiError, auth, type SignupConfig } from "@zonventure/api";
import { useT } from "./i18n.tsx";

/**
 * Everything before the console: log in (with single sign-on when the
 * operator set it up), the second factor, a forgotten password, the
 * reset link from the mail, and sign-up.
 *
 * `reset` is the token from a #/reset?token= link; the screen opens on
 * the new-password form, whether or not anyone is signed in — whoever
 * holds the link is the one who needs it.
 */
type Mode =
  | { kind: "login" }
  | { kind: "totp"; challenge: string; afterReset: boolean }
  | { kind: "forgot"; sentTo?: string }
  | { kind: "reset"; token: string }
  | { kind: "signup" };

export function Login({ onSuccess, resetToken }: { onSuccess: (note?: string) => void; resetToken?: string }) {
  const [mode, setMode] = useState<Mode>(resetToken ? { kind: "reset", token: resetToken } : { kind: "login" });
  const [config, setConfig] = useState<SignupConfig | null>(null);
  useEffect(() => {
    auth.signupConfig().then(setConfig, () => setConfig({ enabled: false }));
  }, []);

  const toLogin = () => setMode({ kind: "login" });
  switch (mode.kind) {
    case "login":
      return <LoginForm config={config} onSuccess={onSuccess} onTOTP={(challenge) => setMode({ kind: "totp", challenge, afterReset: false })} onForgot={() => setMode({ kind: "forgot" })} onSignup={() => setMode({ kind: "signup" })} />;
    case "totp":
      return <TOTPForm challenge={mode.challenge} afterReset={mode.afterReset} onSuccess={onSuccess} onBack={toLogin} />;
    case "forgot":
      return <ForgotForm sentTo={mode.sentTo} onSent={(sentTo) => setMode({ kind: "forgot", sentTo })} onBack={toLogin} />;
    case "reset":
      return <ResetForm token={mode.token} onSuccess={onSuccess} onTOTP={(challenge) => setMode({ kind: "totp", challenge, afterReset: true })} onForgot={() => setMode({ kind: "forgot" })} />;
    case "signup":
      return config && !config.enabled ? (
        <LoginForm config={config} onSuccess={onSuccess} onTOTP={(challenge) => setMode({ kind: "totp", challenge, afterReset: false })} onForgot={() => setMode({ kind: "forgot" })} onSignup={() => {}} />
      ) : (
        <SignupForm onSuccess={onSuccess} onBack={toLogin} />
      );
  }
}

function Card({ sub, children, onSubmit }: { sub?: string; children: ReactNode; onSubmit: (e: FormEvent) => void }) {
  const t = useT();
  return (
    <main className="login">
      <form className="login__card" onSubmit={onSubmit}>
        <div className="login__brand">
          <a className="login__wordmark" href="/" aria-label={t.t("Zonventure home page")}>zonventure</a>
          {sub && <span className="login__sub">{sub}</span>}
        </div>
        {children}
      </form>
    </main>
  );
}

function ErrorLine({ text }: { text: string | null }) {
  // assertive: a failed login must be announced at once.
  return text ? (
    <p className="alert alert--danger" role="alert" aria-live="assertive">
      {text}
    </p>
  ) : null;
}

function LinkButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" className="login__link" onClick={onClick}>
      {children}
    </button>
  );
}

function errorText(t: ReturnType<typeof useT>, err: unknown): string {
  return err instanceof ApiError ? t.tError(err.code, err.message) : t.t("Could not reach the server. Check your connection.");
}

function LoginForm(props: { config: SignupConfig | null; onSuccess: () => void; onTOTP: (challenge: string) => void; onForgot: () => void; onSignup: () => void }) {
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // A lockout's seconds left; the form waits them out rather than
  // letting a repeated tap extend it.
  const [wait, setWait] = useState(0);
  const pw = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (wait <= 0) return;
    const id = setTimeout(() => setWait((s) => s - 1), 1000);
    return () => clearTimeout(id);
  }, [wait]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await auth.login({ username, password });
      if (res?.code === "totp_required" && res.challenge) {
        props.onTOTP(res.challenge);
        return;
      }
      props.onSuccess();
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && err.code === "sso_required") {
        const url = (err.body as { login_url?: string } | undefined)?.login_url;
        if (url) {
          location.href = url;
          return;
        }
      }
      if (err instanceof ApiError && err.status === 429 && err.retryAfter) {
        setWait(err.retryAfter);
      } else {
        setError(err instanceof ApiError && err.status === 401 ? t.t("Wrong username or password.") : errorText(t, err));
      }
      // Keep the username; only the password is typed again.
      setPassword("");
      pw.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  const sso = props.config?.sso?.enabled ? props.config.sso : null;
  return (
    <Card onSubmit={submit}>
      {sso && (
        <div className="login__sso">
          <a className="btn btn--sso" href={sso.login_url || "/auth/oidc/login"}>
            {(sso.provider_name ?? "google") === "google" && <GoogleMark />}
            {sso.button_label || t.t("Sign in with Google")}
          </a>
          {sso.hosted_domain && <p className="login__hint">{t.t("For @{{domain}} accounts.", { domain: sso.hosted_domain })}</p>}
          <div className="login__divider"><span>{t.t("or sign in with password")}</span></div>
        </div>
      )}
      <label className="field">
        <span className="field__label">{t.t("Username or email")}</span>
        <input className="field__input" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required disabled={wait > 0} />
      </label>
      <label className="field">
        <span className="field__label">{t.t("Password")}</span>
        <input ref={pw} className="field__input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required disabled={wait > 0} />
      </label>
      <ErrorLine text={error} />
      {wait > 0 && (
        <p className="alert alert--danger" role="status" aria-live="polite">
          {t.t("Too many failed attempts. Try again in {{n}}s.", { n: wait })}
        </p>
      )}
      <button className="btn btn--primary" type="submit" disabled={busy || wait > 0}>
        {busy ? t.t("Working…") : t.t("Log in")}
      </button>
      <p className="login__links">
        <LinkButton onClick={props.onForgot}>{t.t("Forgot password?")}</LinkButton>
      </p>
      {props.config?.enabled && (
        <p className="login__links">
          {t.t("No account yet?")} <LinkButton onClick={props.onSignup}>{t.t("Sign up")}</LinkButton>
        </p>
      )}
      <p className="login__legal">
        <a href="/privacy">{t.t("Privacy")}</a> · <a href="/terms">{t.t("Terms")}</a>
      </p>
    </Card>
  );
}

function TOTPForm({ challenge, afterReset, onSuccess, onBack }: { challenge: string; afterReset: boolean; onSuccess: (note?: string) => void; onBack: () => void }) {
  const t = useT();
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (recovery === null && code.replace(/\D/g, "").length !== 6) {
      setError(t.t("Enter all 6 digits."));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await auth.loginTOTP(challenge, recovery === null ? { code: code.replace(/\D/g, "") } : { recovery_code: recovery.trim() });
      onSuccess(afterReset ? t.t("Password updated. You're signed in.") : undefined);
    } catch (err) {
      setError(err instanceof ApiError && err.status === 429 && err.retryAfter ? t.t("Too many failed attempts. Try again in {{n}}s.", { n: err.retryAfter }) : errorText(t, err));
      if (recovery === null) setCode("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card sub={afterReset ? t.t("verify 2FA · password reset") : t.t("two-factor")} onSubmit={submit}>
      <p className="login__hint">{t.t("Enter the 6-digit code from your authenticator app.")}</p>
      {recovery === null ? (
        <label className="field">
          <span className="field__label">{t.t("Authentication code")}</span>
          {/* one-time-code lets phones offer the authenticator's value; numeric keeps the keypad up. */}
          <input className="field__input login__otp" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" inputMode="numeric" maxLength={7} autoFocus required />
        </label>
      ) : (
        <label className="field">
          <span className="field__label">{t.t("Recovery code")}</span>
          <input className="field__input" value={recovery} onChange={(e) => setRecovery(e.target.value)} autoComplete="one-time-code" maxLength={11} placeholder="xxxx-xxxx" autoFocus required />
        </label>
      )}
      <ErrorLine text={error} />
      <button className="btn btn--primary" type="submit" disabled={busy}>
        {busy ? t.t("Verifying…") : t.t("Verify")}
      </button>
      <p className="login__links">
        <LinkButton onClick={() => { setRecovery(recovery === null ? "" : null); setError(null); }}>
          {recovery === null ? t.t("Use a recovery code") : t.t("Use the authenticator code")}
        </LinkButton>
      </p>
      <p className="login__links">
        <LinkButton onClick={onBack}>{t.t("Back to log in")}</LinkButton>
      </p>
    </Card>
  );
}

function ForgotForm({ sentTo, onSent, onBack }: { sentTo?: string | undefined; onSent: (email: string) => void; onBack: () => void }) {
  const t = useT();
  const [email, setEmail] = useState(sentTo ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await auth.forgotPassword(email.trim());
      onSent(email.trim());
    } catch (err) {
      setError(errorText(t, err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card sub={t.t("reset password")} onSubmit={submit}>
      <p className="login__hint">{t.t("Enter your email and we'll send you a link to set a new password. The link expires in 1 hour.")}</p>
      <label className="field">
        <span className="field__label">{t.t("Email")}</span>
        <input className="field__input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus required />
      </label>
      <ErrorLine text={error} />
      {sentTo && (
        <p className="alert alert--ok" role="status">
          {t.t("If {{email}} matches an account, a reset link is on its way.", { email: sentTo })}
        </p>
      )}
      <button className="btn btn--primary" type="submit" disabled={busy}>
        {t.t("Send reset link")}
      </button>
      <p className="login__links">
        <LinkButton onClick={onBack}>{t.t("Back to log in")}</LinkButton>
      </p>
    </Card>
  );
}

function ResetForm({ token, onSuccess, onTOTP, onForgot }: { token: string; onSuccess: (note?: string) => void; onTOTP: (challenge: string) => void; onForgot: () => void }) {
  const t = useT();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await auth.resetPassword(token, password);
      // The token is spent either way; a back-button trip must not
      // bring this form back.
      history.replaceState(null, "", "#/");
      if (res?.code === "totp_required" && res.challenge) {
        onTOTP(res.challenge);
        return;
      }
      onSuccess(t.t("Password updated. You're signed in."));
    } catch (err) {
      setError(errorText(t, err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card sub={t.t("reset password")} onSubmit={submit}>
      <label className="field">
        <span className="field__label">{t.t("New password (min 8)")}</span>
        <input className="field__input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={8} autoFocus required />
      </label>
      <ErrorLine text={error} />
      <button className="btn btn--primary" type="submit" disabled={busy}>
        {t.t("Set password")}
      </button>
      <p className="login__links">
        <LinkButton onClick={() => { history.replaceState(null, "", "#/"); onForgot(); }}>{t.t("Request a new link")}</LinkButton>
      </p>
    </Card>
  );
}

function SignupForm({ onSuccess, onBack }: { onSuccess: (note?: string) => void; onBack: () => void }) {
  const t = useT();
  const [email, setEmail] = useState("");
  const [org, setOrg] = useState("");
  // Most people who sign up make their own adventures; the lobby is for
  // those an existing organization will add.
  const [joining, setJoining] = useState(false);
  const [password, setPassword] = useState("");
  const [terms, setTerms] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await auth.signup({ email: email.trim(), org_name: joining ? "" : org.trim(), personal: !joining, password, terms_accepted: terms });
      history.replaceState(null, "", "#/");
      onSuccess(t.t("Account created — check {{email}} for a verification link.", { email: email.trim() }));
    } catch (err) {
      setError(errorText(t, err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card sub={t.t("create account")} onSubmit={submit}>
      <label className="field">
        <span className="field__label">{t.t("Email")}</span>
        <input className="field__input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus required />
      </label>
      <fieldset className="login__choice">
        <legend className="field__label">{t.t("I'm")}</legend>
        <label className="login__check">
          <input type="radio" name="signup-kind" checked={!joining} onChange={() => setJoining(false)} />
          <span>{t.t("making my own adventures")}</span>
        </label>
        <label className="login__check">
          <input type="radio" name="signup-kind" checked={joining} onChange={() => setJoining(true)} />
          <span>{t.t("joining an organization that will add me")}</span>
        </label>
      </fieldset>
      {!joining && (
        <label className="field">
          <span className="field__label">{t.t("Team or company name (optional)")}</span>
          <input className="field__input" value={org} onChange={(e) => setOrg(e.target.value)} autoComplete="organization" />
          <span className="login__hint">{t.t("Leave blank to use your name. You can change it any time in Settings.")}</span>
        </label>
      )}
      <label className="field">
        <span className="field__label">{t.t("Password (min 8)")}</span>
        <input className="field__input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={8} required />
      </label>
      <label className="login__check">
        <input type="checkbox" checked={terms} onChange={(e) => setTerms(e.target.checked)} required />
        <span>
          {t.t("I agree to the ")}
          <a href="/terms" target="_blank" rel="noopener">{t.t("Terms of Service")}</a>
          {t.t(" and ")}
          <a href="/privacy" target="_blank" rel="noopener">{t.t("Privacy Policy")}</a>.
        </span>
      </label>
      <ErrorLine text={error} />
      <button className="btn btn--primary" type="submit" disabled={busy}>
        {t.t("Create account")}
      </button>
      <p className="login__links">
        {t.t("Already registered?")} <LinkButton onClick={onBack}>{t.t("Log in")}</LinkButton>
      </p>
    </Card>
  );
}

/** Google's "G", as its sign-in branding asks for next to the label. */
function GoogleMark() {
  return (
    <svg viewBox="0 0 48 48" width="18" height="18" aria-hidden="true">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  );
}
