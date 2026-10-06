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
  const over = event.expired || event.state === "completed" || event.state === "cancelled";
  const earlier = event.rounds.filter((r) => r.finished_at);
  const date = (s: string) => new Date(s).toLocaleString(i18n.current(), { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  const block = event.price.teams_per_block;
  const canAdd = !over && event.teams_limit + block <= event.max_teams;
  const addTeams = () =>
    run(async () => {
      const r = await api.addBlocks(1);
      // Paid in a tab of its own: this page keeps its link, and shows the
      // new limit once the payment lands.
      if (!r.checkout_url) return;
      const tab = window.open(r.checkout_url, "_blank");
      if (tab) tab.opener = null;
      else location.assign(r.checkout_url);
    });

  return (
    <div className="hosted-game">
      {event.round_open ? (
        <>
          <p className="hosted-game__status">
            <strong>{t("Game {{n}} is being played", { n: event.round })}</strong>{" "}
            <span className="hosted-muted">{t("{{n}} of {{max}} teams", { n: event.teams.length, max: event.teams_limit })}</span>
            {event.state === "paused" && <span className="hosted-pill hosted-pill--paused">{t("Paused")}</span>}
          </p>
          {event.teams.length > 0 && (
            <ul className="hosted-teams">
              {event.teams.map((tm) => (
                <li key={tm.id}>
                  <div>
                    <strong>{tm.name}</strong>
                    <span className="hosted-muted">
                      {t("{{n}} players", { n: tm.headcount })}
                      {" · "}
                      {tm.finished ? t("Finished · {{score}} points", { score: tm.score }) : t("{{score}} points so far", { score: tm.score })}
                    </span>
                  </div>
                  {api.removeTeam && (
                    <button
                      type="button"
                      className="hosted-btn hosted-btn--quiet"
                      disabled={busy}
                      aria-label={t("Remove {{team}}", { team: tm.name })}
                      onClick={() => {
                        if (confirm(t("Take {{team}} out of the game?", { team: tm.name }))) void run(() => api.removeTeam!(tm.id));
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
                <button type="button" className="hosted-btn" disabled={busy} onClick={() => void run(() => api.action!("resume"))}>
                  {t("Resume")}
                </button>
              ) : (
                <button type="button" className="hosted-btn" disabled={busy} onClick={() => void run(() => api.action!("pause"))}>
                  {t("Pause")}
                </button>
              ))}
            <button
              type="button"
              className="hosted-btn hosted-btn--primary"
              disabled={busy}
              onClick={() => {
                if (confirm(t("Finish game {{n}}? The teams' phones stop, and you get the results by email.", { n: event.round }))) void run(() => api.finishGame());
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
        <p className="hosted-muted">{t("No more games can be played.")}</p>
      ) : (
        <p className="hosted-muted">
          {event.round === 0
            ? t("No game yet. The first team to join through the player link starts game 1.")
            : t("No game is being played. The next team to join starts game {{n}}.", { n: event.round + 1 })}
        </p>
      )}

      {canAdd && (
        <p className="hosted-muted">
          {t("Up to {{n}} teams can play in one game.", { n: event.teams_limit })}{" "}
          <button type="button" className="hosted-link-btn" disabled={busy} onClick={() => void addTeams()}>
            {event.price.block_cents > 0
              ? t("Add {{n}} teams for {{price}}", { n: block, price: money(event.price.block_cents, event.price.currency, i18n.current()) })
              : t("Add {{n}} teams", { n: block })}
          </button>
        </p>
      )}

      {earlier.length > 0 && (
        <>
          <h3 className="hosted-sub">{t("Earlier games")}</h3>
          <ul className="hosted-rounds">
            {earlier.map((r) => (
              <li key={r.round}>
                <strong>{t("Game {{n}}", { n: r.round })}</strong>{" "}
                <span className="hosted-muted">
                  {date(r.started_at)} · {t("{{teams}} teams, {{players}} players", { teams: r.teams, players: r.players })}
                </span>
              </li>
            ))}
          </ul>
          <p className="hosted-muted">{t("The results of each game were emailed to you when it finished.")}</p>
        </>
      )}
    </div>
  );
}
