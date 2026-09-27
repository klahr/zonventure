import { useEffect, useRef, useState, type ReactNode } from "react";
import { Markdown, PlayView, StartCeremony, brandVars, metresBetween, type Translate } from "@zonventure/play-ui";
import type { play } from "@zonventure/api";
import { useT } from "./i18n.tsx";
import { useGame } from "./useGame.ts";
import { Inbox } from "./Inbox.tsx";
import { useGeo, useLiveRefresh } from "./live.ts";

export function App() {
  const i18n = useT();
  const t = i18n.t;
  const game = useGame(t);
  const s = game.state;
  const view = s?.view;
  const running = s?.event.state === "running";
  // The team's language for a translated adventure also switches the
  // app's own words, where the app has them.
  const teamLang = view?.lang;
  useEffect(() => {
    if (teamLang && i18n.available().includes(teamLang) && i18n.current() !== teamLang) i18n.setLocale(teamLang);
  }, [teamLang, i18n]);

  useLiveRefresh(game.status === "ready", game.refresh);
  const geo = useGeo(
    game.status === "ready" && running && !view?.finished,
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
    }
  }, [game.status, s, view]);

  // Lane events are played in the original player.
  useEffect(() => {
    if (game.status === "lanes") location.replace("/play");
  }, [game.status]);

  if (game.status === "loading" || game.status === "lanes") {
    return <Screen><p className="app-muted">{t("Loading…")}</p></Screen>;
  }
  if (game.status === "signed-out") {
    return (
      <Screen>
        <h1>{t("Open your invitation link")}</h1>
        <p>{t("Use the link you were sent to join your team. It signs you in on this phone.")}</p>
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
    if (geo.error) return t("Turn on location sharing so we can tell when you're there.");
    if (!geo.pos) return t("Finding your location…");
    const m = metresBetween(geo.pos.coords.latitude, geo.pos.coords.longitude, zone.lat, zone.lng);
    return m < 1000
      ? t("About {{m}} m away", { m: Math.max(5, Math.round(m / 5) * 5) })
      : t("About {{km}} km away", { km: (m < 10_000 ? Math.round(m / 100) / 10 : Math.round(m / 1000)).toLocaleString(i18n.current()) });
  };
  const zoneNote = view?.zone && !view.here ? placeNote(view.zone) : undefined;

  return (
    <div className="app">
      <header className="app-bar">
        {s.event.org_logo_url && <img className="app-bar__logo" src={s.event.org_logo_url} alt={s.event.org_name ?? ""} />}
        <div className="app-bar__who">
          <div className="app-bar__title">{s.event.adventure_title || "Zonventure"}</div>
          <div className="app-bar__team">{s.team.name}</div>
        </div>
        {(!game.online || game.queued > 0) && (
          <span className="app-status" role="status">
            {game.queued > 0 ? t("{{n}} waiting to send", { n: game.queued }) : t("Offline")}
          </span>
        )}
        <Inbox messages={s.messages ?? []} teamID={s.team.id} t={t} locale={i18n.current()} />
      </header>

      <main className="app-main">
        {s.event.state === "paused" && <p className="app-banner">{t("The game is paused. Hold on.")}</p>}
        {game.status === "ended" && <p className="app-banner">{t("This event has ended. Thanks for playing!")}</p>}

        {view ? (
          <PlayView
            view={view}
            onAction={game.act}
            busy={game.busy || !running}
            feedback={game.feedback}
            t={t}
            brand={s.brand ?? null}
            {...(s.event.outro_text ? { outro: s.event.outro_text } : {})}
            {...(zoneNote ? { zoneNote } : {})}
            placeNote={placeNote}
            {...(geo.pos ? { position: { lat: geo.pos.coords.latitude, lng: geo.pos.coords.longitude } } : {})}
            teamName={s.team.name}
            storageKey={`team-${s.team.id}`}
            photo={(block, file) => game.photo(view.node ?? "", block, file)}
            {...(s.messages?.[0] ? { bulletin: s.messages[0] } : {})}
          />
        ) : (
          <Lobby state={s} t={t} />
        )}
      </main>
      {ceremony && <StartCeremony title={s.event.adventure_title || "Zonventure"} brand={s.brand ?? null} onDone={() => setCeremony(false)} />}
    </div>
  );
}

function Lobby({ state, t }: { state: play.PlayState; t: Translate }) {
  const at = state.event.scheduled_at ? new Date(state.event.scheduled_at) : null;
  if (state.event.state === "cancelled") {
    return <p>{t("This event has been cancelled.")}</p>;
  }
  if (state.event.state === "completed") {
    return <p>{t("This event has ended. Thanks for playing!")}</p>;
  }
  return (
    <div className="app-lobby" style={brandVars(state.brand)}>
      <h1>{t("You're on team {{team}}", { team: state.team.name })}</h1>
      <p>
        {at && !Number.isNaN(at.getTime())
          ? t("The game starts {{when}}.", { when: at.toLocaleString([], { weekday: "long", hour: "2-digit", minute: "2-digit" }) })
          : t("The game starts when your organiser says go.")}
      </p>
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
