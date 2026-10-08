import { useEffect, useState } from "react";
import { ApiError } from "@zonventure/api";
import { coverOf } from "@zonventure/play-ui";
import { getJoin, join, type JoinInfo } from "./api.ts";
import { useT } from "./i18n.tsx";
import { useLook } from "./look.ts";
import { randomTeamName } from "./teamName.ts";

/**
 * The join page a hosted event's player link opens: the team names
 * itself, or rolls a name, and goes to the game on this phone.
 */
export function Join({ token }: { token: string }) {
  const i18n = useT();
  const t = i18n.t;
  const [info, setInfo] = useState<JoinInfo | null>(null);
  const [gone, setGone] = useState(false);
  const [team, setTeam] = useState("");
  // The name came from the dice: when it is taken, roll another.
  const [rolled, setRolled] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useLook(info?.look);

  useEffect(() => {
    getJoin(token).then(setInfo, (e) => {
      if (e instanceof ApiError && e.status === 404) setGone(true);
    });
  }, [token]);

  if (gone) {
    return (
      <section className="party-card">
        <h1>{t("This link doesn't open a game")}</h1>
        <p>{t("Ask the host for the link again.")}</p>
      </section>
    );
  }
  if (!info) return <p className="party-muted">{t("Loading…")}</p>;

  const closed = {
    ended: t("This game has ended."),
    full: t("This game is full. Ask the host to add teams."),
    not_ready: t("The host is still setting the game up. Try again in a moment."),
    open: null,
  }[info.status];

  const submit = (name: string, rolls: number) => {
    setBusy(true);
    setError(null);
    join(token, name).then(
      (r) => location.assign(r.play_url),
      (e) => {
        if (rolled && rolls < 3 && e instanceof ApiError && e.code === "name_taken") {
          const next = randomTeamName(i18n.current(), name);
          setTeam(next);
          submit(next, rolls + 1);
          return;
        }
        setBusy(false);
        setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
      },
    );
  };

  const cover = coverOf(info.theme, info.cover_url);
  return (
    <section className={"party-card" + (cover ? " party-card--cover" : "")}>
      {cover && <div className="party-cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
      {info.adventure_title !== info.name && <p className="party-eyebrow">{info.adventure_title}</p>}
      <h1>{info.name}</h1>
      {closed ? (
        <p className="party-note">{closed}</p>
      ) : (
        <form
          className="party-form"
          onSubmit={(e) => {
            e.preventDefault();
            submit(team.trim(), 0);
          }}
        >
          <p className="party-muted">{t("Play on one phone per team. Give your team a name to start.")}</p>
          <div className="party-field">
            <span id="join-team">{t("Team name")}</span>
            <div className="party-roll">
              <input
                aria-labelledby="join-team"
                value={team}
                maxLength={64}
                autoFocus
                onChange={(e) => {
                  setTeam(e.target.value);
                  setRolled(false);
                }}
                required
              />
              <button
                type="button"
                onClick={() => {
                  setTeam(randomTeamName(i18n.current(), team));
                  setRolled(true);
                }}
                aria-label={t("Roll a team name")}
                title={t("Roll a team name")}
              >
                🎲
              </button>
            </div>
          </div>
          {error && (
            <p className="party-error" role="alert">
              {error}
            </p>
          )}
          <button className="party-btn party-btn--primary party-btn--wide" disabled={busy || !team.trim()}>
            {t("Start")}
          </button>
        </form>
      )}
    </section>
  );
}
