import { useEffect, useState } from "react";
import { auth, siteWordmark, type Whoami } from "@zonventure/api";
import { SPOTS } from "@zonventure/ui";
import { I18nProvider, useT } from "./i18n.tsx";
import { SessionProvider, useSession } from "./session.tsx";
import { Login } from "./Login.tsx";
import { Shell } from "./Shell.tsx";
import { ToastProvider, useToast } from "./ui/index.ts";
import { rememberAuthorize, resumeAuthorize } from "./pages/authorize/index.tsx";

export function App() {
  return (
    <I18nProvider>
      <SessionProvider>
        <ToastProvider>
          <Gate />
        </ToastProvider>
      </SessionProvider>
    </I18nProvider>
  );
}

/**
 * The one-shot links mails and the server send here — a password reset,
 * an email change, a verified email, a failed single sign-on. Read once
 * at load; each is then replaced by the dashboard so a refresh doesn't
 * repeat it. The old console's hashes (#reset?token=…) and this app's
 * (#/reset?token=…) both count.
 */
function oneShot(): { reset?: string; changeEmail?: string; verified?: boolean; ssoError?: string; cancelled?: boolean } {
  const h = location.hash.replace(/^#\/?/, "");
  // The classic console's in-app manual is the standalone page now.
  if (h === "manual") location.replace("/manual");
  if (h.startsWith("purchase-cancelled")) return { cancelled: true };
  const reset = h.match(/^reset\?token=([^&]+)/);
  if (reset) return { reset: decodeURIComponent(reset[1]!) };
  const ce = h.match(/^change-email\?token=([^&]+)/);
  if (ce) return { changeEmail: decodeURIComponent(ce[1]!) };
  if (h === "verify-ok") return { verified: true };
  if (h === "verify-failed") return { verified: false };
  const sso = h.match(/^sso-error=([a-z_]+)$/);
  if (sso) return { ssoError: sso[1]! };
  return {};
}

rememberAuthorize();
const once = oneShot();
if (once.changeEmail || once.verified !== undefined || once.ssoError || once.cancelled) history.replaceState(null, "", "#/");

/**
 * Route on session state alone.
 *
 * "anonymous" renders the login form rather than an error, because a
 * 401 from whoami is the ordinary logged-out case. Only a genuine
 * failure — network, 5xx — gets the error branch, with a retry that
 * re-runs whoami instead of forcing a page reload.
 */
function Gate() {
  const t = useT();
  const toast = useToast();
  const { state, refresh } = useSession();
  const [reset, setReset] = useState(once.reset);

  // The one-shot notices, once the session is known.
  const [announced, setAnnounced] = useState(false);
  useEffect(() => {
    if (announced || state.status === "loading") return;
    setAnnounced(true);
    if (once.changeEmail) {
      auth.confirmEmailChange(once.changeEmail).then(
        (r) => {
          toast.ok(t.t("Email updated to {{email}}.", { email: r?.email ?? "" }));
          void refresh();
        },
        (e) => toast.err(e),
      );
    }
    if (once.verified === true) {
      toast.ok(t.t("Email verified — you can now schedule events and buy event passes."));
      void refresh();
    }
    if (once.verified === false) toast.err(t.t('Verification link is invalid or has expired. Sign in and tap "Resend email".'));
    if (once.ssoError) toast.err(ssoMessage(t, once.ssoError));
    if (once.cancelled) toast.ok(t.t("Checkout cancelled — nothing was charged."));
  }, [announced, state.status, t, toast, refresh]);

  const signedIn = (note?: string) => {
    setReset(undefined);
    void refresh();
    if (note) toast.ok(note);
  };

  if (reset) return <Login resetToken={reset} onSuccess={signedIn} />;

  switch (state.status) {
    case "loading":
      return (
        <main className="centered" aria-busy="true">
          <div className="login__brand" aria-hidden="true">
            <span className="login__wordmark">{siteWordmark()}</span>
          </div>
          <p className="lede loading-pulse">{t.t("Loading…")}</p>
        </main>
      );

    case "anonymous":
      return <Login onSuccess={signedIn} />;

    case "authenticated":
      resumeAuthorize();
      // Someone in several organisations picks which to act as first.
      if (state.user.role !== "admin" && !state.user.org_id && state.user.memberships.length > 1) {
        return <OrgPicker user={state.user} />;
      }
      return <Shell user={state.user} />;

    case "error":
      return (
        <main className="centered">
          <img className="centered__art" src={SPOTS.wentWrong} alt="" width={480} height={480} />
          <div className="alert alert--danger" role="alert">
            <strong>{t.t("Something went wrong")}</strong>
            <p>{state.message}</p>
          </div>
          <button className="btn btn--primary" onClick={() => void refresh()}>
            {t.t("Try again")}
          </button>
        </main>
      );
  }
}

function ssoMessage(t: ReturnType<typeof useT>, code: string): string {
  const known: Record<string, string> = {
    domain_not_allowed: t.t("That account is not part of the configured workspace."),
    email_unverified: t.t("Your identity provider didn’t mark your email as verified."),
    idp_error: t.t("The sign-in provider declined the request. Try again, or contact your admin."),
    session_expired: t.t("Sign-in took too long and the session expired. Try again."),
    link_refused: t.t("An account already exists with that email and a local password. Sign in with your password, or ask an admin to remove your local password before signing in with SSO."),
  };
  return known[code] ?? t.t("Single sign-on failed ({{code}}). Try again, or contact your admin.", { code });
}

/** Which organisation to act as, for a user in several. Switchable later from the account menu. */
function OrgPicker({ user }: { user: Whoami }) {
  const t = useT();
  const toast = useToast();
  const { refresh, logout } = useSession();
  const pick = async (orgID: number) => {
    try {
      await auth.switchOrg(orgID);
      await refresh();
    } catch (e) {
      toast.err(e);
    }
  };
  return (
    <main className="login">
      <div className="login__card">
        <div className="login__brand">
          <span className="login__wordmark">{siteWordmark()}</span>
          <span className="login__sub">{t.t("select organization")}</span>
        </div>
        <p className="login__hint">
          {t.t("You belong to {{n}} organizations. Pick which one to act as — you can switch from the menu later.", { n: user.memberships.length })}
        </p>
        <div className="login__orgs">
          {user.memberships.map((m) => (
            <button key={m.org_id} type="button" className="login__org" onClick={() => void pick(m.org_id)}>
              {m.org_logo_url ? <img src={m.org_logo_url} alt="" /> : <span aria-hidden="true">{(m.org_name || "?").trim().charAt(0).toUpperCase()}</span>}
              <strong>{m.org_name}</strong>
              <em>{m.role === "org_admin" ? t.t("admin") : t.t("event host")}</em>
            </button>
          ))}
        </div>
        <button type="button" className="btn btn--ghost" onClick={() => void logout()}>
          {t.t("Log out")}
        </button>
      </div>
    </main>
  );
}
