import { useEffect, useState } from "react";
import { ApiError } from "@zonventure/api";
import { coverOf } from "@zonventure/play-ui";
import { getJoin, join, type JoinInfo } from "./api.ts";
import { useT } from "./i18n.tsx";

/**
 * The join page a hosted event's player link opens: the team names
 * itself, says how many it is, and goes to the game on this phone.
 */
export function Join({ token }: { token: string }) {
  const t = useT().t;
  const [info, setInfo] = useState<JoinInfo | null>(null);
  const [gone, setGone] = useState(false);
  const [team, setTeam] = useState("");
  const [players, setPlayers] = useState(2);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
            setBusy(true);
            setError(null);
            join(token, team.trim(), players).then(
              (r) => location.assign(r.play_url),
              (e) => {
                setBusy(false);
                setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
              },
            );
          }}
        >
          <p className="party-muted">{t("Play on one phone per team. Give your team a name to start.")}</p>
          <label className="party-field">
            <span>{t("Team name")}</span>
            <input value={team} maxLength={64} autoFocus onChange={(e) => setTeam(e.target.value)} required />
          </label>
          <div className="party-field">
            <span id="join-players">{t("How many are you?")}</span>
            <div className="party-stepper" role="group" aria-labelledby="join-players">
              <button type="button" onClick={() => setPlayers((n) => Math.max(1, n - 1))} aria-label={t("Fewer")}>
                −
              </button>
              <output aria-live="polite">{players}</output>
              <button type="button" onClick={() => setPlayers((n) => Math.min(50, n + 1))} aria-label={t("More")}>
                +
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
