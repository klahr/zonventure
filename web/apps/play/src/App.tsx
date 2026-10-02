import { useEffect, useRef, useState, type ReactNode } from "react";
import { Markdown, PlayView, StartCeremony, brandVars, formatSeconds, loadFont, metresBetween, useEngineClock, withTheme, type Translate } from "@zonventure/play-ui";
import { play, siteName } from "@zonventure/api";
import { useT } from "./i18n.tsx";
import { useGame } from "./useGame.ts";
import { Inbox } from "./Inbox.tsx";
import { Menu } from "./Menu.tsx";
import { CheatSheet, useSevenTaps } from "./Cheat.tsx";
import { useGeo, useLiveRefresh } from "./live.ts";

export function App() {
  const i18n = useT();
  const t = i18n.t;
  const game = useGame(t);
  const s = game.state;
  const view = s?.view;
  // Only worth saying while there are roles to tell apart.
  // Never a secret role: the bar shows over a shoulder.
  // While the players choose their roles (a roles block), a role nobody chose is no one's yet.
  const choosing = !view?.roles?.some((r) => r.final) && (view?.blocks ?? []).some((b) => b.kind === "roles" && b.choose && !b.locked);
  const myRoles = (view?.roles?.length ?? 0) > 1 ? (view?.roles ?? []).filter((r) => r.mine && !r.secret && (r.claimed || !choosing)).map((r) => r.name || r.slug) : [];
  const running = s?.event.state === "running";
  // The team's language for a translated adventure also switches the
  // app's own words, where the app has them.
  const teamLang = view?.lang;
  useEffect(() => {
    if (teamLang && i18n.available().includes(teamLang) && i18n.current() !== teamLang) i18n.setLocale(teamLang);
  }, [teamLang, i18n]);

  // The welcome a player reads before the first step — once per player,
  // so a reload mid-game goes straight back to it.
  const introKey = s ? `zv-play-intro-${s.me?.id ?? `t${s.team.id}`}` : "";
  const [introSeen, setIntroSeen] = useState(() => new Set<string>());
  const seen = !introKey || introSeen.has(introKey) || readFlag(introKey);
  const dismissIntro = () => {
    writeFlag(introKey);
    setIntroSeen((prev) => new Set(prev).add(introKey));
  };
  const showIntro = game.status === "ready" && !!view && !view.finished && !seen;

  // The tab and home-screen name say which game this is.
  const docTitle = s ? [s.event.adventure_title, s.team.name].filter(Boolean).join(" · ") : "";
  useEffect(() => {
    document.title = docTitle || siteName();
  }, [docTitle]);

  // Cheating, for a tester: seven quick taps on the title ask for the
  // adventure's cheat password. It lives only in memory, for this team,
  // until a reload or a sign-out.
  const [cheat, setCheat] = useState<{ team: number; password: string } | null>(null);
  const [cheatOpen, setCheatOpen] = useState(false);
  const cheatPassword = cheat && s && cheat.team === s.team.id ? cheat.password : undefined;
  const tapTitle = useSevenTaps(() => {
    if (s?.event.cheat && !cheatPassword) setCheatOpen(true);
  });
  useEffect(() => {
    if (game.status === "signed-out") setCheat(null);
  }, [game.status]);

  // The whole app wears the palette the play surface does: the
  // adventure's, or the organisation's for an adventure without one,
  // under the step's theme.
  const look = s?.brand ?? s?.org_brand ?? null;
  const chrome = withTheme(look, view?.theme);
  useEffect(() => loadFont(look?.font), [look?.font]);
  useEffect(() => loadFont(look?.heading_font), [look?.heading_font]);
  // The phone's own bar matches the app bar.
  const barColour = chrome?.background;
  useEffect(() => {
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (!meta) return;
    const initial = meta.dataset.initial ?? meta.content;
    meta.dataset.initial = initial;
    meta.content = barColour && /^#[0-9a-f]{6}$/i.test(barColour) ? barColour : initial;
  }, [barColour]);

  // A new step takes focus, so keyboard and screen-reader users start
  // at its top instead of on whatever button they last pressed.
  const mainRef = useRef<HTMLElement>(null);
  const node = view?.node;
  const firstNode = useRef(true);
  useEffect(() => {
    if (!node) return;
    if (firstNode.current) {
      firstNode.current = false;
      return;
    }
    mainRef.current?.focus({ preventScroll: false });
  }, [node]);

  useLiveRefresh(game.status === "ready", game.refresh);
  // Location only when the adventure has places (or the organiser asked
  // for positions): a quiz-only game shouldn't prompt for it or run GPS.
  const usesLocation = !!(s?.event.start || view?.zone || view?.starts?.some((o) => o.zone) || view?.watch_position);
  // The player can pause sharing from the menu; remembered on this phone.
  const [shareLocation, setShareLocationState] = useState(() => !readFlag(SHARE_OFF_KEY));
  const setShareLocation = (on: boolean) => {
    setShareLocationState(on);
    if (on) clearFlag(SHARE_OFF_KEY);
    else writeFlag(SHARE_OFF_KEY);
  };
  const geo = useGeo(
    game.status === "ready" && running && !view?.finished && !showIntro && usesLocation && shareLocation,
    (!!view?.zone && !view.here) || !!view?.starts?.some((o) => o.zone && !o.here) || view?.input?.kind === "search" || !!view?.watch_position,
    game.refresh,
  );
  useRecheck(view, game.refresh);

  // The start moment plays when this tab watched the lobby turn into the
  // game — not on every reload of a game already under way.
  const [ceremony, setCeremony] = useState(false);
  const sawLobby = useRef(false);
  useEffect(() => {
    if (game.status !== "ready" || !s) return;
    if (!view) sawLobby.current = true;
    else if (sawLobby.current) {
      sawLobby.current = false;
      setCeremony(true);
      // The lobby already showed the welcome.
      dismissIntro();
    }
  }, [game.status, s, view]);

  if (game.status === "loading") {
    return <Screen><p className="app-muted">{t("Loading…")}</p></Screen>;
  }
  if (game.status === "signed-out") {
    return (
      <Screen>
        <h1>{t("Open your invitation link")}</h1>
        <p>{t("Use the link you were sent to join your team. It signs you in on this phone.")}</p>
        <RecoverLink t={t} />
      </Screen>
    );
  }
  if (game.status === "error" || !s) {
    return (
      <Screen>
        <h1>{t("Can't reach the game")}</h1>
        <p>{t("Check your connection and try again.")}</p>
        <button className="pv-btn pv-btn--primary" onClick={game.retry}>
          {t("Try again")}
        </button>
      </Screen>
    );
  }

  // How far the team is from a place it has to reach.
  const placeNote = (zone: play.PlayZone): string => {
    if (!shareLocation) return t("Location sharing is off. Turn it on in the menu so we can tell when you're there.");
    if (geo.error?.code === 1) return t("Location is blocked for this page. Allow it in your browser's site settings, then reload.");
    if (!geo.pos) return geo.error ? t("Can't find your location right now. Step into the open or wait a moment.") : t("Finding your location…");
    const m = metresBetween(geo.pos.coords.latitude, geo.pos.coords.longitude, zone.lat, zone.lng);
    return m < 1000
      ? t("About {{m}} m away", { m: Math.max(5, Math.round(m / 5) * 5) })
      : t("About {{km}} km away", { km: (m < 10_000 ? Math.round(m / 100) / 10 : Math.round(m / 1000)).toLocaleString(i18n.current()) });
  };
  const zoneNote = view?.zone && !view.here ? placeNote(view.zone) : undefined;

  return (
    <div className="app" style={brandVars(chrome)}>
      <header className="app-bar">
        <Menu
          state={s}
          t={t}
          i18n={i18n}
          usesLocation={usesLocation}
          shareLocation={shareLocation}
          onShareLocation={setShareLocation}
          onRenamed={game.refresh}
          leave={game.leave}
        />
        {s.event.org_logo_url && <img className="app-bar__logo" src={s.event.org_logo_url} alt={s.event.org_name ?? ""} />}
        <div className="app-bar__who">
          <div className="app-bar__title" onClick={tapTitle}>{s.event.adventure_title || siteName()}</div>
          <div className="app-bar__team">
            {s.team.name}
            {myRoles.length > 0 && <span className="app-bar__role"> · {myRoles.join(", ")}</span>}
          </div>
        </div>
        {cheatPassword && <span className="app-status">{t("Cheating")}</span>}
        {(!game.online || game.queued > 0) && (
          <span className="app-status" role="status">
            {game.queued > 0 ? t("{{n}} waiting to send", { n: game.queued }) : t("Offline")}
          </span>
        )}
        <Inbox messages={s.messages ?? []} teamID={s.team.id} t={t} locale={i18n.current()} />
      </header>

      <main className="app-main" ref={mainRef} tabIndex={-1}>
        {s.event.state === "paused" && <p className="app-banner">{t("The game is paused. Hold on.")}</p>}
        {game.status === "ended" && <p className="app-banner">{t("This event has ended. Thanks for playing!")}</p>}

        {showIntro ? (
          <Intro
            state={s}
            t={t}
            usesLocation={usesLocation}
            onDone={dismissIntro}
          />
        ) : view ? (
          <PlayView
            view={view}
            onAction={game.act}
            busy={game.busy || !running}
            feedback={game.feedback}
            t={t}
            brand={look}
            {...(s.event.outro_text ? { outro: s.event.outro_text } : {})}
            {...(s.finish ? { finish: { ...s.finish, provisional: s.event.state !== "completed" && s.event.state !== "cancelled" } } : {})}
            {...(zoneNote ? { zoneNote } : {})}
            placeNote={placeNote}
            {...(geo.pos ? { position: { lat: geo.pos.coords.latitude, lng: geo.pos.coords.longitude } } : {})}
            teamName={s.team.name}
            storageKey={`team-${s.team.id}`}
            photo={(block, file) => game.photo(view.node ?? "", block, file)}
            giveItem={play.giveItem}
            receiveItem={async (code) => {
              try {
                return await play.receiveItem(code);
              } finally {
                game.refresh();
              }
            }}
            claimRole={async (role, claim) => {
              try {
                await play.claimRole(role, claim);
              } finally {
                game.refresh();
              }
            }}
            {...(s.messages?.[0] ? { bulletin: s.messages[0] } : {})}
            {...(cheatPassword ? { cheat: cheatPassword } : {})}
          />
        ) : (
          <Lobby state={s} t={t} />
        )}
      </main>
      {cheatOpen && (
        <CheatSheet
          t={t}
          onClose={() => setCheatOpen(false)}
          onUnlocked={(password) => {
            setCheat({ team: s.team.id, password });
            setCheatOpen(false);
          }}
        />
      )}
      {ceremony && <StartCeremony title={s.event.adventure_title || siteName()} brand={s.brand ?? null} onDone={() => setCeremony(false)} />}
    </div>
  );
}

function Lobby({ state, t }: { state: play.PlayState; t: Translate }) {
  const at = state.event.scheduled_at ? new Date(state.event.scheduled_at) : null;
  const counting = state.event.state === "scheduled" && !!at && !Number.isNaN(at.getTime());
  const now = useEngineClock(state.event.server_now ?? new Date().toISOString(), counting);
  const startsIn = counting && at ? (at.getTime() - now) / 1000 : null;
  if (state.event.state === "cancelled") {
    return <p>{t("This event has been cancelled.")}</p>;
  }
  if (state.event.state === "completed") {
    return <p>{t("This event has ended. Thanks for playing!")}</p>;
  }
  return (
    <div className="app-lobby">
      <h1>{t("You're on team {{team}}", { team: state.team.name })}</h1>
      <p>
        {at && !Number.isNaN(at.getTime())
          ? t("The game starts {{when}}.", { when: at.toLocaleString([], { weekday: "long", day: "numeric", month: "long", hour: "2-digit", minute: "2-digit" }) })
          : t("The game starts when your organiser says go.")}
      </p>
      {startsIn !== null && startsIn < 86400 && (
        <div className={"pv-countdown" + (startsIn <= 0 ? " is-over" : "")} role="timer">
          <span className="pv-countdown__label">{t("Starts in")}</span>
          <span className="pv-countdown__time">{startsIn <= 0 ? t("Any moment now.") : formatSeconds(startsIn)}</span>
        </div>
      )}
      {state.event.intro_text && <Markdown className="app-lobby__intro" text={state.event.intro_text} />}
      {state.event.start && (
        <p className="app-lobby__start">
          <strong>{t("Meet at")}</strong>{" "}
          <a
            href={`https://www.openstreetmap.org/?mlat=${state.event.start.lat}&mlon=${state.event.start.lng}#map=18/${state.event.start.lat}/${state.event.start.lng}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            {state.event.start.label || t("the starting point")}
          </a>
        </p>
      )}
      <p className="app-muted">{t("Keep this page open. It will change by itself.")}</p>
    </div>
  );
}

/** The welcome before the first step, for a player who joins a game already running. */
function Intro({ state, t, usesLocation, onDone }: { state: play.PlayState; t: Translate; usesLocation: boolean; onDone: () => void }) {
  const name = state.me?.name;
  return (
    <div className="app-lobby">
      <h1>{state.event.adventure_title || siteName()}</h1>
      <p>
        {name
          ? t("You're playing as {{name}} on team {{team}}.", { name, team: state.team.name })
          : t("You're on team {{team}}", { team: state.team.name })}
      </p>
      {state.event.intro_text && <Markdown className="app-lobby__intro" text={state.event.intro_text} />}
      {usesLocation && (
        <p className="app-muted">
          {t("This adventure uses your location to know when your team reaches a place. When your phone asks, allow it.")}
        </p>
      )}
      <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" onClick={onDone}>
        {t("Let's go")}
      </button>
    </div>
  );
}

const SHARE_OFF_KEY = "zv-play-share-location-off";

function clearFlag(key: string) {
  try {
    localStorage.removeItem(key);
  } catch {
    /* private mode: nothing was stored */
  }
}

function readFlag(key: string): boolean {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

function writeFlag(key: string) {
  try {
    localStorage.setItem(key, "1");
  } catch {
    /* private mode / full: the intro just shows again next time */
  }
}

/**
 * Lost the link? A team whose organiser gave its email gets fresh links
 * mailed there. The server answers the same whether or not the address
 * matched, so this says so rather than promising a mail.
 */
function RecoverLink({ t }: { t: Translate }) {
  const [email, setEmail] = useState("");
  const [state, setState] = useState<"idle" | "sending" | "sent" | "error">("idle");
  return (
    <form
      className="app-recover"
      onSubmit={(e) => {
        e.preventDefault();
        if (!email.trim()) return;
        setState("sending");
        play.recoverLink(email.trim()).then(() => setState("sent"), () => setState("error"));
      }}
    >
      <label className="app-menu__label" htmlFor="recover-email">{t("Lost your link? Enter your team's email and we'll send fresh links.")}</label>
      <div className="app-menu__row">
        <input
          id="recover-email"
          className="pv-input"
          type="email"
          inputMode="email"
          autoComplete="email"
          value={email}
          onChange={(e) => { setEmail(e.target.value); setState("idle"); }}
        />
        <button type="submit" className="pv-btn pv-btn--primary" disabled={state === "sending" || !email.trim()}>{t("Send")}</button>
      </div>
      {state === "sent" && <p className="app-muted" role="status">{t("If that address belongs to a team, new links are on their way. Check your inbox.")}</p>}
      {state === "error" && <p className="app-menu__error" role="alert">{t("Couldn't reach the server. Check your connection and try again.")}</p>}
    </form>
  );
}

function Screen({ children }: { children: ReactNode }) {
  return (
    <div className="app">
      <main className="app-main app-main--center">{children}</main>
    </div>
  );
}

/**
 * Ask again when the server says a hidden block could appear by itself —
 * a minute after arriving, say. Timed on the engine's clock: recheck_at
 * and now both come from the server, so a wrong phone clock is harmless.
 */
function useRecheck(view: play.View | undefined, refresh: () => void) {
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const at = view?.recheck_at;
  const now = view?.now;
  useEffect(() => {
    if (!at || !now) return;
    const wait = Math.max(1_000, Date.parse(at) - Date.parse(now) + 500);
    const id = setTimeout(() => refreshRef.current(), wait);
    return () => clearTimeout(id);
  }, [at, now]);
}
