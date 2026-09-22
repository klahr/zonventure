import { I18nProvider, useT } from "./i18n.tsx";
import { SessionProvider, useSession } from "./session.tsx";
import { Login } from "./Login.tsx";
import { Shell } from "./Shell.tsx";

export function App() {
  return (
    <I18nProvider>
      <SessionProvider>
        <Gate />
      </SessionProvider>
    </I18nProvider>
  );
}

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
  const { state, refresh } = useSession();

  switch (state.status) {
    case "loading":
      return (
        <main className="centered" aria-busy="true">
          <p className="lede">{t.t("Loading…")}</p>
        </main>
      );

    case "anonymous":
      return <Login onSuccess={() => void refresh()} />;

    case "authenticated":
      return <Shell user={state.user} />;

    case "error":
      return (
        <main className="centered">
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
