import { useConfirm } from "@zonventure/ui";
import { money, type HostedEvent } from "./api.ts";
import { useHosted } from "./context.tsx";

/**
 * The game being played — its teams, and the button that finishes it —
 * or, between games, a note that the next team to join starts one; and
 * the games played before.
 */
export function Game({ event }: { event: HostedEvent }) {
  const { i18n, api, run, busy } = useHosted();
  const t = i18n.t;
  const [ask, confirmDialog] = useConfirm();
  const over =
    event.expired || event.state === "completed" || event.state === "cancelled";
  const earlier = event.rounds.filter((r) => r.finished_at);
  const date = (s: string) =>
    new Date(s).toLocaleString(i18n.current(), {
      day: "numeric",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
    });
  // A game with no team limit charges each team as it joins.
  const canAdd = (n: number) =>
    !over && event.teams_limit > 0 && event.teams_limit + n <= event.max_teams;
  const addTeams = (n: number) =>
    run(async () => {
      const r = await api.addTeams(n);
      // Paid in a tab of its own: this page keeps its link, and shows the
      // new limit once the payment lands.
      if (!r.checkout_url) return;
      const tab = window.open(r.checkout_url, "_blank");
      if (tab) tab.opener = null;
      else location.assign(r.checkout_url);
    });

  return (
    <div className="hosted-game">
      {confirmDialog}
      {event.round_open ? (
        <>
          <p className="hosted-game__status">
            <strong>
              {t("Game {{n}} is being played", { n: event.round })}
            </strong>{" "}
            <span className="zv-muted">
              {event.teams_limit > 0
                ? t("{{n}} of {{max}} teams", {
                    n: event.teams.length,
                    max: event.teams_limit,
                  })
                : t("{{n}} teams", { n: event.teams.length })}
            </span>
            {event.state === "paused" && (
              <span className="zv-pill zv-pill--paused">
                {t("Paused")}
              </span>
            )}
          </p>
          {event.teams.length > 0 && (
            <ul className="hosted-teams">
              {event.teams.map((tm) => (
                <li key={tm.id}>
                  <div>
                    <strong>{tm.name}</strong>
                    <span className="zv-muted">
                      {tm.headcount === 1 ? t("1 player") : t("{{n}} players", { n: tm.headcount })}
                      {" · "}
                      {tm.finished
                        ? t("Finished · {{score}} points", { score: tm.score })
                        : t("{{score}} points so far", { score: tm.score })}
                    </span>
                  </div>
                  {api.removeTeam && (
                    <button
                      type="button"
                      className="zv-btn zv-btn--quiet"
                      disabled={busy}
                      aria-label={t("Remove {{team}}", { team: tm.name })}
                      onClick={async () => {
                        if (
                          await ask({
                            message: t("Take {{team}} out of the game? Their phone stops playing, and the team is left out of the results.", {
                              team: tm.name,
                            }),
                            confirmLabel: t("Take out"),
                            cancelLabel: t("Cancel"),
                            danger: true,
                          })
                        )
                          void run(() => api.removeTeam!(tm.id));
                      }}
                    >
                      ×
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          <div className="hosted-actions">
            {api.action &&
              (event.state === "paused" ? (
                <button
                  type="button"
                  className="zv-btn"
                  disabled={busy}
                  onClick={() => void run(() => api.action!("resume"))}
                >
                  {t("Resume")}
                </button>
              ) : (
                <button
                  type="button"
                  className="zv-btn"
                  disabled={busy}
                  onClick={() => void run(() => api.action!("pause"))}
                >
                  {t("Pause")}
                </button>
              ))}
            <button
              type="button"
              className="zv-btn zv-btn--primary"
              disabled={busy}
              onClick={async () => {
                // Say so when a team is still out: finishing stops it mid-game.
                const playing = event.teams.filter((tm) => !tm.finished).length;
                const still =
                  playing === 0
                    ? ""
                    : (playing === 1
                        ? t("1 team is still playing.")
                        : t("{{n}} teams are still playing.", { n: playing })) + " ";
                if (
                  await ask({
                    message: still + (event.hold_results
                      ? t(
                          "Finish game {{n}}? The teams' phones stop, the results are revealed on the phones and the live scoreboard, and you get them by email.",
                          { n: event.round },
                        )
                      : t(
                          "Finish game {{n}}? The teams' phones stop, and you get the results by email.",
                          { n: event.round },
                        )),
                    confirmLabel: t("Finish game"),
                    cancelLabel: t("Cancel"),
                  })
                )
                  void run(() => api.finishGame());
              }}
            >
              {t("Finish game")}
            </button>
          </div>
          <p>
            <a href={event.live_url} target="_blank" rel="noopener noreferrer">
              {t("Open the live scoreboard")}
            </a>
          </p>
        </>
      ) : over ? (
        <p className="zv-muted">{t("No more games can be played.")}</p>
      ) : (
        <p className="zv-muted">
          {event.round === 0
            ? t(
                "No game yet. The first team to join through the player link starts game 1.",
              )
            : t(
                "No game is being played. The next team to join starts game {{n}}.",
                { n: event.round + 1 },
              )}
        </p>
      )}

      {!over && (
        <label className="hosted-check">
          <input
            type="checkbox"
            checked={event.hold_results}
            disabled={busy}
            onChange={(e) => {
              const hold = e.target.checked;
              void run(() => api.holdResults(hold));
            }}
          />
          <span>
            {t("Keep the results secret until I finish the game")}
            <small className="zv-muted">
              {t(
                "The phones and the live scoreboard show no points or places while the teams play. Finish the game to reveal them.",
              )}
            </small>
          </span>
        </label>
      )}

      {event.round_open && (event.turned_away ?? 0) > 0 && (
        <p className="zv-note" role="status">
          {event.turned_away === 1
            ? t("1 team couldn't join: the game is full.")
            : t("{{n}} teams couldn't join: the game is full.", { n: event.turned_away ?? 0 })}{" "}
          {canAdd(1) && t("Add more teams below.")}
        </p>
      )}

      {canAdd(1) && (
        <p className="zv-muted">
          {t("Up to {{n}} teams can play in one game, one phone per team and any number of players in each.", {
            n: event.teams_limit,
          })}{" "}
          {[1, 5].filter(canAdd).map((n) => (
            <button
              key={n}
              type="button"
              className="zv-link hosted-link-btn"
              disabled={busy}
              onClick={() => void addTeams(n)}
            >
              {event.price.team_cents > 0
                ? t(n === 1 ? "Add a team for {{price}}" : "Add {{n}} teams for {{price}}", {
                    n,
                    price: money(
                      n * event.price.team_cents,
                      event.price.currency,
                      i18n.current(),
                    ),
                  })
                : t(n === 1 ? "Add a team" : "Add {{n}} teams", { n })}
            </button>
          ))}
        </p>
      )}

      {earlier.length > 0 && (
        <>
          <h3 className="hosted-sub">{t("Earlier games")}</h3>
          <ul className="hosted-rounds">
            {earlier.map((r) => (
              <li key={r.round}>
                <details>
                  <summary>
                    <strong>{t("Game {{n}}", { n: r.round })}</strong>{" "}
                    <span className="zv-muted">
                      {date(r.started_at)} ·{" "}
                      {t("{{teams}} teams, {{players}} players", {
                        teams: r.teams,
                        players: r.players,
                      })}
                    </span>
                  </summary>
                  {r.results && r.results.length > 0 ? (
                    <ol className="hosted-teams">
                      {[...r.results]
                        .sort((a, b) => b.score - a.score)
                        .map((tm) => (
                          <li key={tm.id}>
                            <strong>{tm.name}</strong>
                            <span className="zv-muted">
                              {tm.finished
                                ? t("Finished · {{score}} points", { score: tm.score })
                                : t("{{score}} points", { score: tm.score })}
                            </span>
                          </li>
                        ))}
                    </ol>
                  ) : (
                    <p className="zv-muted">{t("No teams played this game.")}</p>
                  )}
                </details>
              </li>
            ))}
          </ul>
          <p className="zv-muted">
            {t(
              "The results of each game were emailed to you when it finished.",
            )}
          </p>
        </>
      )}
    </div>
  );
}
