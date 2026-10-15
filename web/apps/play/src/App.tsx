import { useEffect, useRef, useState, type ReactNode } from "react";
import { FitMap, Markdown, PlayView, Sheet, StartCeremony, coverOf, brandVars, formatSeconds, loadFont, metresBetween, useEngineClock, withTheme, type Translate } from "@zonventure/play-ui";
import { play, siteName } from "@zonventure/api";
import { SPOTS } from "@zonventure/ui";
import { useT } from "./i18n.tsx";
import { useGame } from "./useGame.ts";
import { Inbox } from "./Inbox.tsx";
import { DevBar } from "./DevBar.tsx";
import { Menu, NameField } from "./Menu.tsx";
import { Files } from "./Files.tsx";
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
  // The team switching a translated adventure's language also switches
  // the app's own words, where the app has them. Only a switch: on load
  // the language this phone chose, or its own, holds.
  const languages = view?.languages;
  const teamLang = (languages?.length ?? 0) > 1 ? view?.lang : undefined;
  const lastTeamLang = useRef(teamLang);
  useEffect(() => {
    const prev = lastTeamLang.current;
    lastTeamLang.current = teamLang;
    if (prev && teamLang && prev !== teamLang && i18n.available().includes(teamLang)) i18n.setLocale(teamLang);
  }, [teamLang, i18n]);
  // A translated adventure starts in the phone's language: once per team
  // on this phone, while the team is still in the main language and has
  // not begun, so a team that has chosen or is under way keeps its own.
  const actRef = useRef(game.act);
  actRef.current = game.act;
  const teamID = s?.team.id;
  const begun = !!s?.team.locked;
  const langKey = languages?.join(",") ?? "";
  // A store ticket plays in the language its buyer chose at checkout,
  // and the app's words follow it there rather than the phone's: once
  // per team on this phone, so a player's own pick in the menu holds.
  const chosen = s?.team.lang;
  const playing = teamLang || chosen;
  useEffect(() => {
    if (!teamID || !chosen || !playing) return;
    const key = `zv-play-ui-lang-${teamID}`;
    if (readFlag(key)) return;
    writeFlag(key);
    if (i18n.available().includes(playing)) i18n.setLocale(playing);
  }, [teamID, chosen, playing, i18n]);
  useEffect(() => {
    const langs = langKey ? langKey.split(",") : [];
    if (!teamID || chosen || langs.length < 2 || !running || begun) return;
    const key = `zv-play-lang-${teamID}`;
    if (readFlag(key)) return;
    writeFlag(key);
    const want = phoneLanguage(langs);
    if (want && want !== langs[0] && teamLang === langs[0]) actRef.current({ kind: "language", lang: want });
  }, [teamID, langKey, running, begun, teamLang, chosen]);

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
  // adventure's cheat password. It is kept for this team in this browser
  // session only, so it survives a reload but not closing the tab or a
  // sign-out.
  const [cheat, setCheat] = useState<{ team: number; password: string } | null>(readCheat);
  const [cheatOpen, setCheatOpen] = useState(false);
  const cheatPassword = cheat && s && cheat.team === s.team.id ? cheat.password : undefined;
  const tapTitle = useSevenTaps(() => {
    if (s?.event.cheat && !cheatPassword) setCheatOpen(true);
  });
  useEffect(() => {
    if (game.status !== "signed-out") return;
    setCheat(null);
    writeCheat(null);
  }, [game.status]);

  // The whole app wears the palette the play surface does: the
  // adventure's, or the organisation's for an adventure without one,
  // under the step's theme.
  const look = s?.brand ?? s?.org_brand ?? null;
  const chrome = withTheme(look, view?.theme, view?.theme_look);
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
  // A party's places are cards to scan, never somewhere to reach.
  const usesLocation = !view?.stations && !!(s?.event.start || s?.event.uses_location || view?.zone || view?.starts?.some((o) => o.zone) || view?.watch_position);

  // A party's card scanned with the phone's own camera opens
  // /play?station=CODE: take the team there once the game is up.
  const pendingStation = useRef(new URLSearchParams(location.search).get("station"));
  // Why a link that was just opened signed no one in (see MagicLinkLogin).
  const [linkProblem] = useState(() => new URLSearchParams(location.search).get("link"));
  useEffect(() => {
    const code = pendingStation.current;
    if (!code || game.status !== "ready" || !running || !view) return;
    pendingStation.current = null;
    history.replaceState(null, "", location.pathname + location.hash);
    game.act({ kind: "station", node: view.node ?? "", guess: code });
  }, [game, running, view]);
  // The player can pause sharing from the menu; remembered on this phone.
  const [shareLocation, setShareLocationState] = useState(() => !readFlag(SHARE_OFF_KEY));
  const setShareLocation = (on: boolean) => {
    setShareLocationState(on);
    if (on) clearFlag(SHARE_OFF_KEY);
    else {
      writeFlag(SHARE_OFF_KEY);
      // Where the phone last was no longer counts as being there.
      void play.locationOff().then(game.refresh, () => {});
    }
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
    const over = game.over ?? (linkProblem === "cancelled" ? "cancelled" : null);
    if (over === "replaced") {
      return (
        <Screen>
          <h1>{t("Opened on another phone")}</h1>
          <p>{t("Your link was opened on another phone, which now plays as you. This phone is signed out. To play here again, open your link on this phone.")}</p>
        </Screen>
      );
    }
    if (over === "taken_out" || over === "game_finished") {
      return <PartyEnd reason={over} t={t} />;
    }
    if (over) {
      return (
        <Screen>
          <h1>{over === "cancelled" ? t("This event has been cancelled.") : t("This event has ended. Thanks for playing!")}</h1>
        </Screen>
      );
    }
    return (
      <Screen>
        <img className="app-spot" src={SPOTS.invitation} alt="" width={480} height={480} />
        <h1>{t("Open your invitation link")}</h1>
        {game.erased && <p role="status">{t("Your play data from this event is erased, and your link no longer works.")}</p>}
        {linkProblem === "invalid" && <p className="app-menu__error" role="alert">{t("That link doesn't work any more. It may have been replaced by a newer one, or its play data erased: use the latest link you were sent, or ask for fresh links below.")}</p>}
        {linkProblem === "expired" && <p className="app-menu__error" role="alert">{t("That link has expired. Ask your organiser for a new one, or ask for fresh links below.")}</p>}
        {linkProblem === "busy" && <p className="app-menu__error" role="alert">{t("Too many tries just now. Wait a minute, then open your link again.")}</p>}
        {linkProblem === "unpaid" && <p className="app-menu__error" role="alert">{t("This game can't start just now. Ask the organiser, then open your link again.")}</p>}
        <p>{t("Use the link you were sent to join your team. It signs you in on this phone.")}</p>
        <RecoverLink t={t} />
      </Screen>
    );
  }
  if (game.status === "error" || !s) {
    return (
      <Screen>
        <img className="app-spot" src={SPOTS.wentWrong} alt="" width={480} height={480} />
        <h1>{t("Can't reach the game")}</h1>
        <p>{t("Check your connection and try again.")}</p>
        <button className="pv-btn pv-btn--primary" onClick={game.retry}>
          {t("Try again")}
        </button>
      </Screen>
    );
  }

  // How far the team is from a place it has to reach.
  const placeNote = (zone: play.PlayZone): ReactNode => {
    if (!shareLocation) return t("Location sharing is off. Turn it on in the menu so we can tell when you're there.");
    if (geo.error?.code === 1) {
      return (
        <>
          {t("Location is blocked for this page. Allow location for this site in your browser's settings, then reload. Still stuck? Ask the organisers to move your team on.")}{" "}
          <button type="button" className="pv-btn" onClick={() => location.reload()}>{t("Reload")}</button>
        </>
      );
    }
    if (!geo.pos) return geo.error ? t("Can't find your location right now. Step into the open or wait a moment.") : t("Finding your location…");
    const m = metresBetween(geo.pos.coords.latitude, geo.pos.coords.longitude, zone.lat, zone.lng);
    return m < 1000
      ? t("About {{m}} m away", { m: Math.max(5, Math.round(m / 5) * 5) })
      : t("About {{km}} km away", { km: (m < 10_000 ? Math.round(m / 100) / 10 : Math.round(m / 1000)).toLocaleString(i18n.current()) });
  };
  const zoneNote = view?.zone && !view.here && !view.zone.hidden ? placeNote(view.zone) : undefined;

  return (
    <div className="app" style={brandVars(chrome)}>
      {/* The dev bar first, at the very top; it and the bar stay on screen together. */}
      <div className="app-top">
        {s.dev && <DevBar dev={s.dev} node={s.view?.node} t={t} onMoved={game.refresh} />}
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
          {/* The adventure's own picture, else the organisation's logo. */}
          {s.event.adventure_icon_url ? (
            <img className="app-bar__logo" src={s.event.adventure_icon_url} alt={s.event.adventure_title ?? ""} />
          ) : (
            s.event.org_logo_url && <img className="app-bar__logo" src={s.event.org_logo_url} alt={s.event.org_name ?? ""} />
          )}
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
              {[!game.online ? t("Offline") : null, game.queued > 0 ? t("{{n}} waiting to send", { n: game.queued }) : null].filter(Boolean).join(" · ")}
            </span>
          )}
          <Inbox messages={s.messages ?? []} teamID={s.team.id} t={t} locale={i18n.current()} />
        </header>
      </div>
      <main className="app-main" ref={mainRef} tabIndex={-1}>
        {s.event.state === "paused" && <p className="app-banner">{t("The game is paused. Hold on.")}</p>}
        {game.status === "ended" && s.event.state !== "cancelled" && view?.finished && <p className="app-banner">{t("This event has ended. Thanks for playing!")}</p>}
        {s.moved_on && view && !view.finished && <p className="app-banner">{t("The organisers moved your team on to this step.")}</p>}

        {/* A team that had not finished when the event ended keeps no step to act on. */}
        {s.event.state === "cancelled" || (game.status === "ended" && !view?.finished) ? (
          <EventOver state={s} t={t} />
        ) : showIntro ? (
          <Intro
            state={s}
            t={t}
            usesLocation={usesLocation}
            onDone={dismissIntro}
            onRenamed={game.refresh}
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
            {...(s.finish ? { finish: { ...s.finish, provisional: game.status !== "ended" && s.event.state !== "completed" } } : {})}
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
          <Lobby state={s} t={t} locale={i18n.current()} usesLocation={usesLocation} />
        )}
      </main>
      {cheatOpen && (
        <CheatSheet
          t={t}
          onClose={() => setCheatOpen(false)}
          onUnlocked={(password) => {
            setCheat({ team: s.team.id, password });
            writeCheat({ team: s.team.id, password });
            setCheatOpen(false);
          }}
        />
      )}
      {ceremony && <StartCeremony title={s.event.adventure_title || siteName()} brand={s.brand ?? null} onDone={() => setCeremony(false)} />}
    </div>
  );
}

function Lobby({ state, t, locale, usesLocation }: { state: play.PlayState; t: Translate; locale: string; usesLocation: boolean }) {
  const at = state.event.scheduled_at ? new Date(state.event.scheduled_at) : null;
  const counting = state.event.state === "scheduled" && !!at && !Number.isNaN(at.getTime());
  const now = useEngineClock(state.event.server_now ?? new Date().toISOString(), counting);
  const startsIn = counting && at ? (at.getTime() - now) / 1000 : null;
  const [mapOpen, setMapOpen] = useState(false);
  if (state.event.state === "cancelled" || state.event.state === "completed") {
    return <EventOver state={state} t={t} />;
  }
  const cover = coverOf(state.event.theme, state.event.cover ? `/media/${state.event.cover}` : undefined);
  return (
    <div className={"app-lobby" + (cover ? " app-lobby--cover" : "")}>
      {cover && <div className="app-lobby__cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
      <h1>{t("Your team: {{team}}", { team: state.team.name })}</h1>
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
          {/* On the app's own map: a link out would leave the game. */}
          <button type="button" className="app-lobby__place" onClick={() => setMapOpen(true)}>
            {state.event.start.label || t("the starting point")}
          </button>
        </p>
      )}
      {mapOpen && state.event.start && (
        <div className="pv app-inbox__pv">
          <Sheet title={state.event.start.label || t("the starting point")} t={t} onClose={() => setMapOpen(false)}>
            <div className="pv-sheet__body">
              <FitMap className="pv-map" zone={{ slug: "start", lat: state.event.start.lat, lng: state.event.start.lng, radius_m: state.event.start.radius_m || 25 }} />
            </div>
          </Sheet>
        </div>
      )}
      {state.event.start?.radius_m ? <p className="app-muted">{t("You can only begin once you're there.")}</p> : null}
      {usesLocation && <p className="app-muted">{t("This adventure uses your location to know when your team reaches a place. When your phone asks, allow it.")}</p>}
      <Files files={state.event.files ?? []} t={t} locale={locale} />
      <p className="app-muted">{t("Keep this page open. It will change by itself.")}</p>
    </div>
  );
}

/**
 * A party team's end, in the party's look, when the host took it out or
 * finished the game before it did: no step to keep, and no email to ask
 * for links again.
 */
/** The event's end, cancelled or over, as a card like the lobby's. */
function EventOver({ state, t }: { state: play.PlayState; t: Translate }) {
  const cancelled = state.event.state === "cancelled";
  const cover = coverOf(state.event.theme, state.event.cover ? `/media/${state.event.cover}` : undefined);
  return (
    <div className={"app-lobby" + (cover ? " app-lobby--cover" : "")}>
      {cover && <div className="app-lobby__cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
      {state.event.adventure_title && <p className="app-muted">{state.event.adventure_title}</p>}
      <h1>{cancelled ? t("This event has been cancelled.") : t("This event has ended. Thanks for playing!")}</h1>
      <p>{t("Your team: {{team}}", { team: state.team.name })}</p>
      {cancelled && <p className="app-muted">{t("Questions? Contact the organisers.")}</p>}
    </div>
  );
}

function PartyEnd({ reason, t }: { reason: play.PartyEnd["reason"]; t: Translate }) {
  const [end, setEnd] = useState<play.PartyEnd | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    play.getPartyEnd().then(setEnd, () => setFailed(true));
  }, []);
  useEffect(() => loadFont(end?.brand?.font), [end?.brand?.font]);
  useEffect(() => loadFont(end?.brand?.heading_font), [end?.brand?.heading_font]);
  const cover = end ? coverOf(end.theme, end.cover ? `/media/${end.cover}` : undefined) : undefined;
  // The session's code can't tell the host finishing the game from it
  // ending by itself; the server's answer can. Without one, say neither.
  const why = end?.reason ?? (reason === "taken_out" ? reason : failed ? "game_over" : null);
  const takenOut = why === "taken_out";
  const headings: Record<play.PartyEnd["reason"], string> = {
    taken_out: t("You've been taken out of the game"),
    game_finished: t("The host has finished the game"),
    game_over: t("The game is over"),
  };
  return (
    <div className="app" style={brandVars(end?.brand)}>
      <main className="app-main app-main--center">
        <div className={"app-lobby" + (cover ? " app-lobby--cover" : "")}>
          {cover && <div className="app-lobby__cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
          {end?.adventure_title && <p className="app-muted">{end.adventure_title}</p>}
          {why ? <h1>{headings[why]}</h1> : <p className="app-muted">{t("Loading…")}</p>}
          {end && <p>{t("Your team: {{team}}", { team: end.team_name })}</p>}
          {takenOut && <p className="app-muted">{t("Talk to the host if this was a mistake.")}</p>}
          {end && !takenOut && (end.held ? (
            <p>{t("The host will share the results")}</p>
          ) : (end.points !== undefined || end.rank) ? (
            <div className="pv-stats">
              {end.points !== undefined && (
                <div className="pv-stat">
                  <div className="pv-stat__v">{Math.round(end.points * 10) / 10}</div>
                  <div className="pv-stat__l">{t("Points")}</div>
                </div>
              )}
              {end.rank ? (
                <div className="pv-stat">
                  <div className="pv-stat__v">
                    {end.rank}
                    {end.teams ? <span className="pv-stat__of"> / {end.teams}</span> : null}
                  </div>
                  <div className="pv-stat__l">{t("Place")}</div>
                </div>
              ) : null}
            </div>
          ) : null)}
        </div>
      </main>
    </div>
  );
}

/** The welcome before the first step, for a player who joins a game already running. */
function Intro({ state, t, usesLocation, onDone, onRenamed }: { state: play.PlayState; t: Translate; usesLocation: boolean; onDone: () => void; onRenamed: () => void }) {
  // A party's one phone per team plays under the team's own name.
  const own = state.me?.name !== state.team.name ? state.me?.name : undefined;
  // A ticket's slots are stored as "Player 1".."Player N", or in the buyer's language; show that default in the player's.
  const numbered = own && /^(?:Player|Spelare) (\d+)$/.exec(own);
  const name = numbered ? t("Player {{n}}", { n: numbered[1]! }) : own;
  const cover = coverOf(state.event.theme, state.event.cover ? `/media/${state.event.cover}` : undefined);
  return (
    <div className={"app-lobby" + (cover ? " app-lobby--cover" : "")}>
      {cover && <div className="app-lobby__cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
      <h1>{state.event.adventure_title || siteName()}</h1>
      <p>
        {name
          ? t("You're playing as {{name}} on team {{team}}.", { name, team: state.team.name })
          : t("Your team: {{team}}", { team: state.team.name })}
      </p>
      {state.event.intro_text && <Markdown className="app-lobby__intro" text={state.event.intro_text} />}
      {/* Until it begins, the team can swap a given name such as "Team 2" for its own. */}
      {own && !state.team.locked && (
        <div className="app-lobby__name">
          <NameField label={t("Team name")} value={state.team.name} t={t} save={play.renameTeam} onSaved={onRenamed} locked={false} hint={t("Everyone on the team sees the change.")} />
        </div>
      )}
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
const CHEAT_KEY = "zv-play-cheat";

function readCheat(): { team: number; password: string } | null {
  try {
    const c = JSON.parse(sessionStorage.getItem(CHEAT_KEY) ?? "null") as { team?: unknown; password?: unknown } | null;
    return c && typeof c.team === "number" && typeof c.password === "string" ? { team: c.team, password: c.password } : null;
  } catch {
    return null;
  }
}

function writeCheat(c: { team: number; password: string } | null) {
  try {
    if (c) sessionStorage.setItem(CHEAT_KEY, JSON.stringify(c));
    else sessionStorage.removeItem(CHEAT_KEY);
  } catch {
    /* private mode: cheating ends with a reload */
  }
}

/** The first of the phone's languages the adventure is in ("en-GB" finds "en"), if any. */
function phoneLanguage(langs: string[]): string | undefined {
  const lower = langs.map((l) => l.toLowerCase());
  for (const tag of navigator.languages ?? []) {
    const t = tag.toLowerCase();
    let i = lower.indexOf(t);
    if (i < 0) i = lower.indexOf(t.split("-")[0] ?? "");
    if (i >= 0) return langs[i];
  }
  return undefined;
}

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
      <label className="app-menu__label" htmlFor="recover-email">{t("Lost your link? Enter your team's email and we'll send your links again.")}</label>
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
      {state === "sent" && <p className="app-muted" role="status">{t("If that address belongs to a team in a game that hasn't ended, your links are on their way. Check your inbox.")}</p>}
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
