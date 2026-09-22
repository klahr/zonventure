import { useState } from "react";
import type { Whoami } from "@zonventure/api";
import { readTheme, setTheme, type Theme } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import { useSession } from "./session.tsx";
import { href, useRoute, type Route } from "./router.ts";
import { AdventuresPage } from "./AdventuresPage.tsx";
import { Designer } from "./designer/Designer.tsx";

/**
 * Authenticated shell: sidebar, header and the content region.
 *
 * Navigation links only the screens that exist. Role-specific items
 * (organisations and logs for a global admin) return with their views —
 * a link that 403s or goes nowhere is worse than no link.
 */
export function Shell({ user }: { user: Whoami }) {
  const t = useT();
  const { logout } = useSession();
  const [theme, setThemeState] = useState<Theme>(() => readTheme());

  const route = useRoute();
  // Only screens that exist are linked. A nav item that goes nowhere is
  // worse than a missing one; the rest arrive as the console is ported.
  const nav: { label: string; to: Route; active: boolean }[] = [
    { label: "Dashboard", to: { name: "dashboard" }, active: route.name === "dashboard" },
    { label: "Adventures", to: { name: "adventures" }, active: route.name === "adventures" || route.name === "designer" },
  ];

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
          {nav.map((item) => (
            <a
              key={item.label}
              className={"shell__link" + (item.active ? " is-active" : "")}
              href={href(item.to)}
              {...(item.active ? { "aria-current": "page" as const } : {})}
            >
              {t.t(item.label)}
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

      <main className={"shell__main" + (route.name === "designer" ? " shell__main--full" : "")} id="main">
        {/* Leaving a seeded credential on a reachable box is a
            takeover, so this is loud and not dismissible. */}
        {user.password_is_default && route.name !== "designer" && (
          <div className="alert alert--danger" role="alert">
            <strong>{t.t("Default password in use.")}</strong>{" "}
            {t.t(
              "This account still uses a seeded password. Change it now — anyone reaching this URL can log in as you until you do.",
            )}
          </div>
        )}

        {route.name === "designer" && <Designer key={route.slug} slug={route.slug} />}
        {route.name === "adventures" && <AdventuresPage />}
        {route.name === "dashboard" && (
          <>
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
              <a href={href({ name: "adventures" })}>{t.t("Design an adventure →")}</a>
            </p>
          </>
        )}
      </main>
    </div>
  );
}
