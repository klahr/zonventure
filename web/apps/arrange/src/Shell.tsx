import { lazy, Suspense, useEffect, useRef, useState, type ComponentType } from "react";
import { createPortal } from "react-dom";
import { Menu as MenuIcon, X, Building2, CalendarDays, ChevronLeft, ChevronRight, ChevronUp, Compass, Gauge, Gift, LogOut, Map as MapIcon, Monitor, Moon, Settings, Sun, Terminal, type LucideIcon } from "lucide-react";
import { auth, emails, siteName, siteWordmark, type Whoami } from "@zonventure/api";
import { readTheme, setTheme, type Theme } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import { useSession } from "./session.tsx";
import { href, useRoute, type Route } from "./router.ts";
import { TopBarSlot, TopBarTitle } from "./topbar.tsx";
import { Avatar, DialogProvider, Loading, PersonAvatar, ToastProvider } from "./ui/index.ts";
import "./designer/dz.css";
import "./ui/console.css";
import { DashboardPage } from "./pages/dashboard/index.tsx";
import { AccountBanners } from "./pages/settings/Banners.tsx";

// Every page but the dashboard loads when first opened: the designer
// alone — React Flow, the inspector, the simulator — is most of the
// console, and an organiser on a phone should not wait for it to see
// their events.
// lazy() takes any component; the page keeps its own props.
type AnyComponent = ComponentType<any>;
function page<M, K extends keyof M>(load: () => Promise<M>, name: K) {
  return lazy(async () => {
    try {
      return { default: (await load())[name] as Extract<M[K], AnyComponent> };
    } catch (e) {
      // A tab opened before a deploy asks for chunks that are gone:
      // load the new build instead — once, so a real outage still shows.
      if (reloadOnce()) return new Promise<never>(() => {});
      throw e;
    }
  });
}

const RELOADED_KEY = "zonventure.reloaded-for-build";
function reloadOnce(): boolean {
  try {
    const last = Number(sessionStorage.getItem(RELOADED_KEY) ?? 0);
    if (Date.now() - last < 60_000) return false;
    sessionStorage.setItem(RELOADED_KEY, String(Date.now()));
  } catch {
    return false;
  }
  location.reload();
  return true;
}
const Designer = page(() => import("./designer/Designer.tsx"), "Designer");
const EventsPage = page(() => import("./pages/events/index.tsx"), "EventsPage");
const QuestsPage = page(() => import("./pages/events/index.tsx"), "QuestsPage");
const EventPage = page(() => import("./pages/event/index.tsx"), "EventPage");
const EventControlPage = page(() => import("./pages/control/index.tsx"), "EventControlPage");
const AdventuresPage = page(() => import("./pages/adventures/index.tsx"), "AdventuresPage");
const AdventurePage = page(() => import("./pages/adventures/index.tsx"), "AdventurePage");
const CatalogPage = page(() => import("./pages/adventures/index.tsx"), "CatalogPage");
const CatalogItemPage = page(() => import("./pages/adventures/index.tsx"), "CatalogItemPage");
const FieldPage = page(() => import("./pages/adventures/index.tsx"), "FieldPage");
const AuthorizePage = page(() => import("./pages/authorize/index.tsx"), "AuthorizePage");
const SettingsPage = page(() => import("./pages/settings/index.tsx"), "SettingsPage");
const LogsPage = page(() => import("./pages/admin/index.tsx"), "LogsPage");
const OrganizationsPage = page(() => import("./pages/admin/index.tsx"), "OrganizationsPage");
const OrganizationPage = page(() => import("./pages/admin/index.tsx"), "OrganizationPage");
const WebhookLogPage = page(() => import("./pages/admin/index.tsx"), "WebhookLogPage");
const RedemptionsPage = page(() => import("./pages/redemptions/index.tsx"), "RedemptionsPage");

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
 * list in arrange.html: an admin's operator tools (organisations and
 * the system — they author nothing), a member's daily ones, or —
 * before joining an organisation — only the way out. `adventures`
 * marks the section the designer belongs to.
 */
type Section = "dashboard" | "organizations" | "logs" | "settings" | "adventures" | "events" | "quests" | "redemptions";

// Settings is not listed here: the account menu at the foot of the
// sidebar leads there, on every screen.
function consoleNav(user: Whoami): { label: string; icon: LucideIcon; href: string; section: Section }[] {
  const item = (label: string, icon: LucideIcon, r: Route, section: Section) => ({ label, icon, href: href(r), section });
  if (user.role === "admin") {
    return [
      item("Dashboard", Gauge, { name: "dashboard" }, "dashboard"),
      item("Organizations", Building2, { name: "organizations" }, "organizations"),
      item("Logs", Terminal, { name: "logs" }, "logs"),
    ];
  }
  if (!user.memberships.length) return [item("Dashboard", Gauge, { name: "dashboard" }, "dashboard")];
  return [
    item("Dashboard", Gauge, { name: "dashboard" }, "dashboard"),
    item("Adventures", MapIcon, { name: "adventures" }, "adventures"),
    item("Events", CalendarDays, { name: "events" }, "events"),
    item("Self-guided", Compass, { name: "quests" }, "quests"),
    item("Box redemptions", Gift, { name: "redemptions" }, "redemptions"),
  ];
}

/** The sidebar section a route belongs to, and its crumb label. */
function sectionOf(r: Route): { section: Section; label: string } {
  switch (r.name) {
    case "adventures":
    case "adventure":
    case "designer":
    case "field":
    case "catalog":
    case "catalog-item":
      return { section: "adventures", label: "Adventures" };
    case "events":
    case "event":
    case "event-control":
      return { section: "events", label: "Events" };
    case "quests":
      return { section: "quests", label: "Self-guided" };
    case "redemptions":
      return { section: "redemptions", label: "Box redemptions" };
    case "organizations":
    case "organization":
      return { section: "organizations", label: "Organizations" };
    case "logs":
      return { section: "logs", label: "Logs" };
    case "settings":
    case "webhook-log":
      return { section: "settings", label: "Settings" };
    default:
      return { section: "dashboard", label: "Dashboard" };
  }
}

/**
 * Who may open what, as the classic console decides it: the platform
 * admin runs organisations and the system, and authors or runs nothing;
 * a user in no organisation yet has the dashboard and settings only.
 */
function allowed(r: Route, user: Whoami): boolean {
  const s = sectionOf(r).section;
  if (user.role === "admin") return s === "dashboard" || s === "organizations" || s === "logs" || s === "settings";
  if (!user.memberships.length) return s === "dashboard" || s === "settings";
  return s !== "organizations" && s !== "logs";
}

export function Shell({ user }: { user: Whoami }) {
  const t = useT();
  const { logout } = useSession();
  // dazyflow's rail: the choice sticks between visits.
  const [collapsed, setCollapsed] = useState(savedCollapsed);
  // The designer names its adventure here, as the crumb after the
  // wordmark — the same "Section / page" the classic console's top bar shows.
  const [pageTitle, setPageTitle] = useState<string | null>(null);

  const route = useRoute();
  const current = sectionOf(route);
  const nav = consoleNav(user).map((item) => ({ ...item, active: item.section === current.section }));
  // Each page names itself for the crumb; a new page starts unnamed.
  const routeKey = href(route);
  useEffect(() => setPageTitle(null), [routeKey]);
  // On a phone the sidebar is a drawer from the top bar's ☰; any move
  // to another page closes it.
  const [drawer, setDrawer] = useState(false);
  useEffect(() => setDrawer(false), [routeKey]);
  // A page this role can't open goes to the dashboard, crumb and all.
  const permitted = allowed(route, user);
  useEffect(() => {
    if (!permitted) location.replace("#/");
  }, [permitted, routeKey]);
  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawer(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawer]);

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
  // The notifications this person gets are written in the language they
  // use the console in: tell the server whenever that changes.
  const lang = t.current();
  useEffect(() => {
    void emails.setMyLanguage(lang).catch(() => {
      /* the next change or visit tells it again */
    });
  }, [lang]);
  // Where a page shows who else has it open.
  const [liveSlot, setLiveSlot] = useState<HTMLElement | null>(null);

  return (
    <div className={"shell" + (route.name === "designer" ? " shell--designer" : "")} data-nav-collapsed={collapsed}>
      <a className="zv-skip-link" href="#main">
        {t.t("Skip to main content")}
      </a>

      <header className="shell__top">
        <button
          type="button"
          className="shell__menu"
          onClick={() => setDrawer((d) => !d)}
          aria-expanded={drawer}
          aria-controls="shell-sidebar"
          aria-label={drawer ? t.t("Close menu") : t.t("Menu")}
        >
          {drawer ? <X size={20} aria-hidden /> : <MenuIcon size={20} aria-hidden />}
        </button>
        <a className="shell__brand" href={href({ name: "dashboard" })} title={siteName()} aria-label={siteName()}>
          <span className="shell__wordmark" aria-hidden="true">{siteWordmark()}</span>
          {/* On a phone the mark alone, to leave the bar to the breadcrumb. */}
          <svg className="shell__mark" viewBox="0 0 314 266" aria-hidden="true">
            <path className="shell__mark-p" d="M14.0,12.7Q9.0,4.0 19.0,4.0L59.0,4.0Q69.0,4.0 74.0,12.7L99.2,57.0L154.6,57.0L97.0,158.0Z" />
            <path className="shell__mark-o" d="M239.8,12.7Q244.8,4.0 254.8,4.0L294.8,4.0Q304.8,4.0 299.8,12.7L217.0,158.0L157.0,158.0Z" />
            <path className="shell__mark-d" d="M131.8,218.7Q126.9,210.0 136.9,210.0L176.1,210.0Q186.1,210.0 181.2,218.7L156.5,262.0Z" />
          </svg>
        </a>
        {current.section !== "dashboard" && (
          <nav className="shell__crumb" aria-label={t.t("Breadcrumb")}>
            <a href={nav.find((n) => n.section === current.section)?.href ?? href({ name: "dashboard" })}>{t.t(current.label)}</a>
            {pageTitle && (
              <>
                <span aria-hidden="true">/</span>
                <span className="shell__crumb-page" aria-current="page" title={pageTitle}>
                  {pageTitle}
                </span>
              </>
            )}
          </nav>
        )}
        <span className="shell__live" ref={setLiveSlot} />
        {org && (
          <span className="shell__org">
            <Avatar name={org.org_name} src={org.org_logo_url} size={22} />
            <span className="shell__org-name">{org.org_name}</span>
          </span>
        )}
      </header>

      {drawer && <div className="shell__backdrop" onClick={() => setDrawer(false)} aria-hidden="true" />}
      <aside id="shell-sidebar" className="shell__sidebar" data-collapsed={collapsed} data-open={drawer}>

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

      <main className={"shell__main dz" + (route.name === "designer" ? " shell__main--full" : "")} id="main">
        {/* Leaving a seeded credential on a reachable box is a
            takeover, so this is loud and not dismissible. */}
        {user.password_is_default && route.name !== "designer" && (
          <div className="alert alert--danger" role="alert">
            <strong>{t.t("Default password in use.")}</strong>{" "}
            {t.t(
              "This account still uses a seeded password. Change it now — anyone reaching this URL can log in as you until you do.",
            )}{" "}
            <a href={href({ name: "settings", tab: "account" })}>{t.t("Change password →")}</a>
          </div>
        )}
        <AccountBanners user={user} />
        <TopBarTitle.Provider value={setPageTitle}>
          <TopBarSlot.Provider value={liveSlot}>
            <ToastProvider>
              <DialogProvider>
                <Suspense fallback={<Loading label={t.t("Loading…")} />}>
                  {permitted ? <Page route={route} /> : <DashboardPage />}
                </Suspense>
              </DialogProvider>
            </ToastProvider>
          </TopBarSlot.Provider>
        </TopBarTitle.Provider>
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

  // The Account tab can change these too; read them afresh on opening.
  const { refresh } = useSession();
  const [version, setVersion] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setThemeState(readTheme());
    if (version === null) {
      fetch("/api/version")
        .then((r) => r.json() as Promise<{ version?: string }>)
        .then((v) => setVersion(v.version ?? ""), () => setVersion(""));
    }
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
        {user.avatar_url ? (
          <PersonAvatar name={user.username} src={user.avatar_url} size={32} className="account-menu__avatar" />
        ) : (
          <span className="account-menu__avatar" aria-hidden>
            {user.username.slice(0, 1).toUpperCase()}
          </span>
        )}
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
            {user.memberships.length > 1 && (
              <>
                <div className="account-pop__sep" role="separator" />
                <div className="account-pop__label">{t.t("Organization")}</div>
                {user.memberships.map((m) => (
                  <button
                    key={m.org_id}
                    type="button"
                    role="menuitemradio"
                    aria-checked={m.org_id === user.org_id}
                    className={"account-pop__row" + (m.org_id === user.org_id ? " is-on" : "")}
                    onClick={() => {
                      setOpen(false);
                      if (m.org_id !== user.org_id) void auth.switchOrg(m.org_id).then(() => { location.hash = "#/"; return refresh(); });
                    }}
                  >
                    <Avatar name={m.org_name} src={m.org_logo_url} size={18} />
                    {m.org_name}
                  </button>
                ))}
              </>
            )}
            <div className="account-pop__sep" role="separator" />
            <a role="menuitem" className="account-pop__row" href={href({ name: "settings" })} onClick={() => setOpen(false)}>
              <Settings size={14} aria-hidden />
              {t.t("Settings")}
            </a>
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
            <div className="account-pop__foot">
              <a href="/privacy">{t.t("Privacy")}</a> · <a href="/terms">{t.t("Terms")}</a>
              {version && <span className="account-pop__version">{version}</span>}
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}

/** Each language in its own words, so a reader can find theirs. */
const LANGUAGE: Record<string, string> = { en: "English", sv: "Svenska" };

/** The page for a route. Each area's pages live under pages/<area>/. */
function Page({ route }: { route: Route }) {
  switch (route.name) {
    case "dashboard":
      return <DashboardPage />;
    case "adventures":
      return <AdventuresPage />;
    case "adventure":
      return <AdventurePage key={route.slug} slug={route.slug} />;
    case "designer":
      return <Designer key={route.slug} slug={route.slug} />;
    case "field":
      return <FieldPage />;
    case "authorize":
      return <AuthorizePage />;
    case "catalog":
      return <CatalogPage />;
    case "catalog-item":
      return <CatalogItemPage key={route.slug} slug={route.slug} />;
    case "events":
      return <EventsPage />;
    case "event":
      return <EventPage key={route.id} id={route.id} />;
    case "event-control":
      return <EventControlPage key={route.id} id={route.id} />;
    case "quests":
      return <QuestsPage />;
    case "redemptions":
      return <RedemptionsPage tab={route.tab} />;
    case "organizations":
      return <OrganizationsPage />;
    case "organization":
      return <OrganizationPage key={route.id} id={route.id} />;
    case "logs":
      return <LogsPage />;
    case "settings":
      return <SettingsPage tab={route.tab} />;
    case "webhook-log":
      return <WebhookLogPage key={route.endpoint} endpoint={route.endpoint} />;
    case "not-found":
      return <DashboardPage />;
  }
}
