import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Building2, CalendarDays, ChevronLeft, ChevronRight, ChevronUp, Compass, Gauge, LogOut, Map as MapIcon, Monitor, Moon, Settings, Sun, Terminal, type LucideIcon } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { readTheme, setTheme, type Theme } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import { useSession } from "./session.tsx";
import { consoleHref, useRoute } from "./router.ts";
import { AdventuresPage } from "./AdventuresPage.tsx";
import { Designer } from "./designer/Designer.tsx";

/**
 * Authenticated shell: sidebar, header and the content region.
 *
 * The sidebar is the classic console's, section for section and role
 * for role: this console holds the designer, and every other section
 * still lives at /arrange, so its links go there. Moving between the two
 * then feels like one app — the designer is simply one of its pages.
 */
const COLLAPSE_KEY = "zonventure.sidebar.collapsed";

function savedCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === "1";
  } catch {
    return false;
  }
}

const ROLE: Record<Whoami["role"], string> = { admin: "Administrator", org_admin: "Organisation admin", org_member: "Member" };

/**
 * The classic console's sidebar sections for this user, mirroring the
 * list in arrange.html: an admin's cross-organisation tools, a member's
 * daily ones, or — before joining an organisation — only the way out.
 * `adventures` marks the section the designer belongs to.
 */
function consoleNav(user: Whoami): { label: string; icon: LucideIcon; href: string; adventures: boolean }[] {
  const item = (label: string, icon: LucideIcon, hash: string, adventures = false) => ({ label, icon, href: consoleHref(hash), adventures });
  if (user.role === "admin") {
    return [
      item("Dashboard", Gauge, "dashboard"),
      item("Organizations", Building2, "organizations"),
      item("Events", CalendarDays, "events"),
      item("Quests", Compass, "quests"),
      item("Library", MapIcon, "library", true),
      item("Logs", Terminal, "logs"),
      item("Settings", Settings, "settings"),
    ];
  }
  if (!user.memberships.length) return [item("Dashboard", Gauge, "dashboard"), item("Settings", Settings, "settings")];
  return [
    item("Dashboard", Gauge, "dashboard"),
    item("Adventures", MapIcon, "adventures", true),
    item("Events", CalendarDays, "events"),
    item("Quests", Compass, "quests"),
    item("Settings", Settings, "settings"),
  ];
}

export function Shell({ user }: { user: Whoami }) {
  const t = useT();
  const { logout } = useSession();
  // dazyflow's rail: the choice sticks between visits.
  const [collapsed, setCollapsed] = useState(savedCollapsed);

  const route = useRoute();
  const nav = consoleNav(user).map((item) => ({ ...item, active: item.adventures && route.name !== "dashboard" }));

  function toggle() {
    setCollapsed((c) => {
      try {
        localStorage.setItem(COLLAPSE_KEY, c ? "0" : "1");
      } catch {
        /* private mode: the choice lasts this visit */
      }
      return !c;
    });
  }

  const org = user.memberships.find((m) => m.org_id === user.org_id);

  return (
    <div className="shell" data-nav-collapsed={collapsed}>
      <a className="zv-skip-link" href="#main">
        {t.t("Skip to main content")}
      </a>

      <aside className="shell__sidebar" data-collapsed={collapsed}>
        <a className="shell__brand" href={consoleHref("dashboard")} title="Zonventure Studio">
          <span className="shell__wordmark">{collapsed ? "z" : "zonventure"}</span>
          <span className="shell__sub nav-label">STUDIO</span>
        </a>

        <nav className="shell__nav" aria-label={t.t("Primary")}>
          {nav.map((item) => (
            <a
              key={item.label}
              className={"shell__link" + (item.active ? " is-active" : "")}
              href={item.href}
              title={t.t(item.label)}
              {...(item.active ? { "aria-current": "page" as const } : {})}
            >
              <item.icon size={18} aria-hidden />
              <span className="nav-label">{t.t(item.label)}</span>
            </a>
          ))}
        </nav>

        <div className="shell__spacer" />

        <button type="button" className="shell__collapse" onClick={toggle} aria-expanded={!collapsed} title={collapsed ? t.t("Expand sidebar") : undefined}>
          {collapsed ? <ChevronRight size={16} aria-hidden /> : <ChevronLeft size={16} aria-hidden />}
          <span className="nav-label">{t.t("Collapse sidebar")}</span>
        </button>

        <AccountMenu user={user} orgName={org?.org_name} collapsed={collapsed} onLogout={() => void logout()} />
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
              <a href={consoleHref("adventures")}>{t.t("Design an adventure →")}</a>
            </p>
          </>
        )}
      </main>
    </div>
  );
}

/**
 * The account menu at the foot of the sidebar, as in dazyflow: who you
 * are, and a menu that opens upwards. Theme and language sit in the menu
 * itself — dazyflow sends them to a settings page, which the studio does
 * not have yet.
 *
 * Portaled to <body> with fixed coordinates, so neither the sidebar's
 * overflow nor the designer's canvas can clip or cover it.
 */
function AccountMenu({ user, orgName, collapsed, onLogout }: { user: Whoami; orgName: string | undefined; collapsed: boolean; onLogout: () => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [theme, setThemeState] = useState<Theme>(() => readTheme());
  const trigger = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState<{ left: number; bottom: number } | null>(null);

  useEffect(() => {
    if (!open) return;
    const place = () => {
      const r = trigger.current?.getBoundingClientRect();
      if (r) setPos({ left: r.left, bottom: window.innerHeight - r.top + 6 });
    };
    place();
    const onDown = (e: MouseEvent) => {
      const el = e.target as HTMLElement;
      if (!el.closest(".account-menu") && !el.closest(".account-pop")) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open]);

  const changeTheme = (next: Theme) => {
    setTheme(next);
    setThemeState(next);
  };
  const themes: { value: Theme; label: string; icon: LucideIcon }[] = [
    { value: "system", label: "System", icon: Monitor },
    { value: "light", label: "Light", icon: Sun },
    { value: "dark", label: "Dark", icon: Moon },
  ];
  const sub = [t.t(ROLE[user.role]), orgName].filter(Boolean).join(" · ");

  return (
    <div className="account-menu">
      <button
        ref={trigger}
        type="button"
        className="account-menu__trigger"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t.t("Account menu")}
        title={collapsed ? user.username : undefined}
      >
        <span className="account-menu__avatar" aria-hidden>
          {user.username.slice(0, 1).toUpperCase()}
        </span>
        <span className="account-menu__who nav-label">
          <strong>{user.username}</strong>
          {sub && <span>{sub}</span>}
        </span>
        <ChevronUp size={14} className="nav-label" aria-hidden />
      </button>
      {open &&
        pos &&
        createPortal(
          <div className="account-pop" role="menu" style={{ left: pos.left, bottom: pos.bottom }}>
            <div className="account-pop__head">
              <strong>{user.username}</strong>
              {user.email && <span>{user.email}</span>}
            </div>
            <div className="account-pop__sep" role="separator" />
            <div className="account-pop__label">{t.t("Theme")}</div>
            <div className="account-pop__choices" role="group" aria-label={t.t("Theme")}>
              {themes.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  role="menuitemradio"
                  aria-checked={theme === o.value}
                  className={"account-pop__choice" + (theme === o.value ? " is-on" : "")}
                  onClick={() => changeTheme(o.value)}
                >
                  <o.icon size={14} aria-hidden />
                  {t.t(o.label)}
                </button>
              ))}
            </div>
            <div className="account-pop__label">{t.t("Language")}</div>
            <div className="account-pop__choices" role="group" aria-label={t.t("Language")}>
              {t.available().map((l) => (
                <button
                  key={l}
                  type="button"
                  role="menuitemradio"
                  aria-checked={t.current() === l}
                  className={"account-pop__choice" + (t.current() === l ? " is-on" : "")}
                  onClick={() => t.setLocale(l)}
                >
                  {LANGUAGE[l] ?? l.toUpperCase()}
                </button>
              ))}
            </div>
            <div className="account-pop__sep" role="separator" />
            <button
              type="button"
              role="menuitem"
              className="account-pop__row is-danger"
              onClick={() => {
                setOpen(false);
                onLogout();
              }}
            >
              <LogOut size={14} aria-hidden />
              {t.t("Log out")}
            </button>
          </div>,
          document.body,
        )}
    </div>
  );
}

/** Each language in its own words, so a reader can find theirs. */
const LANGUAGE: Record<string, string> = { en: "English", sv: "Svenska" };
