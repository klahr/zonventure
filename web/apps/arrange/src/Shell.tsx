import { useState } from "react";
import { isGlobalAdmin, type Whoami } from "@zonventure/api";
import { readTheme, setTheme, type Theme } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import { useSession } from "./session.tsx";

/**
 * Authenticated shell: sidebar, header and the content region.
 *
 * Navigation differs by role, matching the existing console — a global
 * admin manages organisations and reads logs, an org member does not,
 * and showing links that 403 on click is worse than not showing them.
 */
export function Shell({ user }: { user: Whoami }) {
  const t = useT();
  const { logout } = useSession();
  const [theme, setThemeState] = useState<Theme>(() => readTheme());

  const nav = isGlobalAdmin(user)
    ? ["Dashboard", "Organisations", "Adventures", "Events", "Settings"]
    : ["Dashboard", "Adventures", "Events", "Settings"];

  function changeTheme(next: Theme) {
    setTheme(next);
    setThemeState(next);
  }

  const org = user.memberships.find((m) => m.org_id === user.org_id);

  return (
    <div className="shell">
      <a className="zv-skip-link" href="#main">
        {t.t("Skip to main content")}
      </a>

      <aside className="shell__sidebar">
        <div className="shell__brand">
          <span className="shell__wordmark">zonventure</span>
          <span className="shell__sub">STUDIO</span>
        </div>

        <nav className="shell__nav" aria-label={t.t("Primary")}>
          {nav.map((item, i) => (
            <a
              key={item}
              className={"shell__link" + (i === 0 ? " is-active" : "")}
              href={"#" + item.toLowerCase()}
              {...(i === 0 ? { "aria-current": "page" as const } : {})}
            >
              {t.t(item)}
            </a>
          ))}
        </nav>

        <div className="shell__footer">
          <label className="field field--inline">
            <span className="field__label">{t.t("Theme")}</span>
            <select
              className="field__input"
              value={theme}
              onChange={(e) => changeTheme(e.target.value as Theme)}
            >
              <option value="system">{t.t("System")}</option>
              <option value="light">{t.t("Light")}</option>
              <option value="dark">{t.t("Dark")}</option>
            </select>
          </label>

          <label className="field field--inline">
            <span className="field__label">{t.t("Language")}</span>
            <select
              className="field__input"
              value={t.current()}
              onChange={(e) => t.setLocale(e.target.value)}
            >
              {t.available().map((l) => (
                <option key={l} value={l}>
                  {l.toUpperCase()}
                </option>
              ))}
            </select>
          </label>

          <button className="btn btn--ghost" onClick={() => void logout()}>
            {t.t("Log out")}
          </button>
        </div>
      </aside>

      <main className="shell__main" id="main">
        {/* Leaving a seeded credential on a reachable box is a
            takeover, so this is loud and not dismissible. */}
        {user.password_is_default && (
          <div className="alert alert--danger" role="alert">
            <strong>{t.t("Default password in use.")}</strong>{" "}
            {t.t(
              "This account still uses a seeded password. Change it now — anyone reaching this URL can log in as you until you do.",
            )}
          </div>
        )}

        <h1>{t.t("Dashboard")}</h1>
        <dl className="meta">
          <dt>{t.t("Signed in as {{name}}", { name: "" }).trim()}</dt>
          <dd>
            {user.username} · {user.role}
          </dd>
          {org && (
            <>
              <dt>{t.t("Organisation")}</dt>
              <dd>{org.org_name}</dd>
            </>
          )}
        </dl>

        <p className="lede">
          Phase 1 shell. Authentication, session, theming and i18n are
          live; the console views land in Phase 6.
        </p>
      </main>
    </div>
  );
}
