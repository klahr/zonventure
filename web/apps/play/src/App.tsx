import { useEffect, type ReactNode } from "react";
import { PlayView } from "@zonventure/play-ui";
import { useT } from "./i18n.tsx";
import { useGame } from "./useGame.ts";
import { metresBetween, useGeo, useLiveRefresh } from "./live.ts";

export function App() {
  const i18n = useT();
  const t = (key: string, vars?: Record<string, string | number>) => i18n.t(key, vars);
  const game = useGame(t);
  const s = game.state;
  const view = s?.view;
  const running = s?.event.state === "running";

  useLiveRefresh(game.status === "ready", game.refresh);
  const geo = useGeo(game.status === "ready" && running && !view?.finished, !!view?.zone && !view.here, game.refresh);

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

  let zoneNote: string | undefined;
  if (view?.zone && !view.here) {
    if (geo.error) zoneNote = t("Turn on location sharing so we can tell when you're there.");
    else if (!geo.pos) zoneNote = t("Finding your location…");
    else {
      const m = metresBetween(geo.pos.coords.latitude, geo.pos.coords.longitude, view.zone.lat, view.zone.lng);
      zoneNote =
        m < 1000
          ? t("About {{m}} m away", { m: Math.max(5, Math.round(m / 5) * 5) })
          : t("About {{km}} km away", { km: (m < 10_000 ? Math.round(m / 100) / 10 : Math.round(m / 1000)).toLocaleString(i18n.current()) });
    }
  }

  return (
    <div className="app">
      <header className="app-bar">
        <div>
          <div className="app-bar__title">{s.event.adventure_title || "Zonventure"}</div>
          <div className="app-bar__team">{s.team.name}</div>
        </div>
        {(!game.online || game.queued > 0) && (
          <span className="app-status" role="status">
            {game.queued > 0 ? t("{{n}} waiting to send", { n: game.queued }) : t("Offline")}
          </span>
        )}
      </header>

      <main className="app-main">
        {s.event.state === "paused" && <p className="app-banner">{t("The game is paused. Hold on.")}</p>}

        {view ? (
          <PlayView
            view={view}
            onAction={game.act}
            busy={game.busy || !running}
            feedback={game.feedback}
            t={t}
            {...(zoneNote ? { zoneNote } : {})}
          />
        ) : (
          <Lobby state={s} t={t} />
        )}
      </main>
    </div>
  );
}

function Lobby({ state, t }: { state: NonNullable<ReturnType<typeof useGame>["state"]>; t: (k: string, v?: Record<string, string | number>) => string }) {
  const at = state.event.scheduled_at ? new Date(state.event.scheduled_at) : null;
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
          ? t("The game starts {{when}}.", { when: at.toLocaleString([], { weekday: "long", hour: "2-digit", minute: "2-digit" }) })
          : t("The game starts when your organiser says go.")}
      </p>
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
