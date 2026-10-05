import { useCallback, useEffect, useState } from "react";
import { ApiError } from "@zonventure/api";
import { addTeam, getParty, partyAction, removeTeam, stationQR, teamQR, type Party, type Team } from "./api.ts";
import { Resend } from "./Book.tsx";
import { PersonaliseSection, PlacesSection, WhereSection } from "./Adapt.tsx";
import { useT } from "./i18n.tsx";

type Sheet = "cards" | "teams" | null;

/**
 * The host page: a party's teams, its cards to print and hide, and the
 * controls to start, pause and end it. The link is the host's only key.
 */
export function Host({ token }: { token: string }) {
  const i18n = useT();
  const t = i18n.t;
  const [party, setParty] = useState<Party | null>(null);
  const [gone, setGone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [sheet, setSheet] = useState<Sheet>(null);

  const load = useCallback(() => {
    getParty(token).then(
      (p) => {
        setParty(p);
        setGone(false);
      },
      (e) => {
        if (e instanceof ApiError && e.status === 404) setGone(true);
      },
    );
  }, [token]);

  useEffect(load, [load]);
  // Who has joined and how far each team got: often while it runs.
  const state = party?.state;
  useEffect(() => {
    if (!state || state === "completed" || state === "cancelled") return;
    const id = setInterval(load, state === "scheduled" ? 20_000 : 8_000);
    return () => clearInterval(id);
  }, [state, load]);

  // Print the sheet once it is on the page with its QR codes drawn,
  // then put it away.
  useEffect(() => {
    if (!sheet) return;
    let cancelled = false;
    const done = () => setSheet(null);
    window.addEventListener("afterprint", done, { once: true });
    const images = Array.from(document.querySelectorAll<HTMLImageElement>(".party-print img"));
    void Promise.all(images.map((img) => img.decode().catch(() => undefined))).then(() => {
      if (!cancelled) window.print();
    });
    return () => {
      cancelled = true;
      window.removeEventListener("afterprint", done);
    };
  }, [sheet]);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t("Something went wrong. Try again."));
    } finally {
      setBusy(false);
    }
  };

  if (gone) {
    return (
      <section className="party-card">
        <h1>{t("This link doesn't open a party")}</h1>
        <p>{t("A newer link may have replaced it — use the one in your latest email. A party is also deleted some days after it ends.")}</p>
        <Resend />
      </section>
    );
  }
  if (!party) return <p className="party-muted">{t("Loading…")}</p>;

  const placed = party.teams.reduce((n, tm) => n + tm.size, 0);
  const left = Math.max(party.kids - placed, 0);
  const open = !party.expired && party.state !== "completed" && party.state !== "cancelled";
  const validUntil = new Date(party.valid_until).toLocaleDateString(i18n.current(), { day: "numeric", month: "long" });
  // The steps a host goes through, numbered by what this party needs.
  let step = 0;
  const heading = (title: string) => (
    <h2>
      <span className="party-step">{++step}</span> {title}
    </h2>
  );
  const chooseWhere = party.state === "scheduled" && party.cards_ok && party.park_ok;
  const personal = party.values.length > 0 || party.editables.length > 0;

  return (
    <>
      <div className="party-screen">
        <section className="party-card party-head">
          <p className="party-eyebrow">{party.adventure_title}</p>
          <h1>{party.name}</h1>
          <p className="party-muted">
            <StatePill state={party.state} />{" "}
            {party.state === "scheduled" && !party.expired && t("Start any time before {{date}}.", { date: validUntil })}
          </p>
          {party.expired && <p className="party-note">{t("This party wasn't started in time. Contact {{org}} if you still want to play.", { org: party.org_name })}</p>}
          {party.state === "cancelled" && <p className="party-note">{t("This party was called off.")}</p>}
          {party.state === "completed" && <p className="party-note">{t("The party is over. The results stay open for a while, then everything about the players is deleted.")}</p>}
        </section>

        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}

        {chooseWhere && (
          <section className="party-card">
            {heading(t("Where are you playing?"))}
            <WhereSection token={token} party={party} run={run} busy={busy} />
          </section>
        )}

        <section className="party-card">
          {heading(t("Teams"))}
          <p className="party-muted">
            {t("Booked for {{kids}} children; {{left}} not in a team yet. Each team plays together on one phone or more.", { kids: party.kids, left })}
          </p>
          <ul className="party-teams">
            {party.teams.map((tm) => (
              <TeamRow key={tm.id} token={token} team={tm} running={party.state !== "scheduled"} canRemove={party.state === "scheduled"} busy={busy} onRemove={() => run(() => removeTeam(token, tm.id))} />
            ))}
          </ul>
          {open && left > 0 && <AddTeam key={party.teams.length} next={party.teams.length + 1} left={left} busy={busy} onAdd={(name, size) => run(() => addTeam(token, name, size))} />}
          {party.teams.length > 0 && (
            <button className="party-btn" onClick={() => setSheet("teams")}>
              {t("Print the team codes")}
            </button>
          )}
        </section>

        {party.mode === "park" ? (
          <section className="party-card">
            {heading(t("Places"))}
            <PlacesSection token={token} party={party} run={run} busy={busy} />
          </section>
        ) : (
        <section className="party-card">
          {heading(t("Cards"))}
          {party.stations.length === 0 ? (
            <p className="party-muted">{t("This adventure has no cards to hide.")}</p>
          ) : (
            <>
              <p className="party-muted">
                {t("Print the {{n}} cards and hide them where the children can find them: under a cushion, behind a tree, in the letterbox. The game tells each team which card to look for next.", {
                  n: party.stations.length,
                })}
              </p>
              <ol className="party-cards">
                {party.stations.map((s) => (
                  <li key={s.zone}>
                    <strong>{s.name}</strong> <code>{s.code}</code>
                  </li>
                ))}
              </ol>
              <button className="party-btn party-btn--primary" onClick={() => setSheet("cards")}>
                {t("Print the cards")}
              </button>
            </>
          )}
        </section>
        )}

        {personal && (
          <section className="party-card">
            {heading(t("Make it yours"))}
            <p className="party-muted">{t("The adventure lets you put in your own words. Leave a field as it is to keep the original.")}</p>
            <PersonaliseSection token={token} party={party} run={run} busy={busy} />
          </section>
        )}

        <section className="party-card">
          {heading(t("Play"))}
          {party.state === "scheduled" && (
            <>
              <p className="party-muted">
                {party.mode === "park"
                  ? t("When everyone is there and every place is put, start the game. Each team scans its team code to join.")
                  : t("When everyone is there and the cards are hidden, start the game. Each team scans its team code to join.")}
              </p>
              <button
                className="party-btn party-btn--primary party-btn--wide"
                disabled={busy || !open || party.teams.length === 0}
                onClick={() => run(() => partyAction(token, "start"))}
              >
                {party.teams.length === 0 ? t("Add a team to start") : t("Start the game")}
              </button>
            </>
          )}
          {(party.state === "running" || party.state === "paused") && (
            <div className="party-actions">
              {party.state === "running" ? (
                <button className="party-btn" disabled={busy} onClick={() => run(() => partyAction(token, "pause"))}>
                  {t("Pause")}
                </button>
              ) : (
                <button className="party-btn party-btn--primary" disabled={busy} onClick={() => run(() => partyAction(token, "resume"))}>
                  {t("Resume")}
                </button>
              )}
              <button
                className="party-btn"
                disabled={busy}
                onClick={() => {
                  if (confirm(t("End the game for every team now? This can't be undone."))) void run(() => partyAction(token, "end"));
                }}
              >
                {t("End the game")}
              </button>
            </div>
          )}
          {party.state !== "scheduled" && party.state !== "cancelled" && (
            <p>
              <a href={party.live_url} target="_blank" rel="noopener noreferrer">
                {t("Open the live scoreboard")}
              </a>
            </p>
          )}
        </section>
      </div>

      {sheet && <PrintSheet sheet={sheet} party={party} token={token} />}
    </>
  );
}

function StatePill({ state }: { state: Party["state"] }) {
  const t = useT().t;
  const label = {
    scheduled: t("Not started"),
    running: t("Playing"),
    paused: t("Paused"),
    completed: t("Over"),
    cancelled: t("Called off"),
  }[state];
  return <span className={"party-pill party-pill--" + state}>{label}</span>;
}

function TeamRow({ token, team, running, canRemove, busy, onRemove }: { token: string; team: Team; running: boolean; canRemove: boolean; busy: boolean; onRemove: () => void }) {
  const t = useT().t;
  return (
    <li className="party-team">
      <img src={teamQR(token, team.id)} alt={t("Join code for {{team}}", { team: team.name })} width={96} height={96} />
      <div>
        <strong>{team.name}</strong>
        <span className="party-muted">
          {running && team.finished ? t("Finished · {{score}} points", { score: team.score }) : t("{{joined}} of {{size}} joined", { joined: team.joined, size: team.size })}
        </span>
        <span className="party-muted">
          {t("Or type {{code}} at {{site}}", { code: team.voucher, site: new URL(team.join_url).host + "/play" })}
        </span>
      </div>
      {canRemove && (
        <button className="party-btn party-btn--quiet" disabled={busy} onClick={onRemove} aria-label={t("Remove {{team}}", { team: team.name })}>
          ×
        </button>
      )}
    </li>
  );
}

function AddTeam({ next, left, busy, onAdd }: { next: number; left: number; busy: boolean; onAdd: (name: string, size: number) => void }) {
  const t = useT().t;
  const [name, setName] = useState(t("Team {{n}}", { n: next }));
  const [size, setSize] = useState(Math.min(left, 4));
  return (
    <form
      className="party-inline"
      onSubmit={(e) => {
        e.preventDefault();
        onAdd(name.trim(), size);
      }}
    >
      <label className="party-field">
        <span>{t("Team name")}</span>
        <input value={name} maxLength={64} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label className="party-field party-field--narrow">
        <span>{t("Children")}</span>
        <input type="number" min={1} max={left} value={size} onChange={(e) => setSize(Math.max(1, Math.min(left, Number(e.target.value) || 1)))} />
      </label>
      <button className="party-btn party-btn--primary" disabled={busy || !name.trim()}>
        {t("Add team")}
      </button>
    </form>
  );
}

/** What the printer gets: the cards to hide, or the teams' join codes. */
function PrintSheet({ sheet, party, token }: { sheet: Exclude<Sheet, null>; party: Party; token: string }) {
  const t = useT().t;
  return (
    <div className="party-print">
      {sheet === "cards"
        ? party.stations.map((s, i) => (
            <div className="party-print__card" key={s.zone}>
              <span className="party-print__no">{i + 1}</span>
              <strong>{s.name}</strong>
              <img src={stationQR(token, s.zone)} alt="" width={180} height={180} />
              <code>{s.code}</code>
              <small>{t("Scan me in the game")}</small>
            </div>
          ))
        : party.teams.map((tm) => (
            <div className="party-print__card" key={tm.id}>
              <strong>{tm.name}</strong>
              <img src={teamQR(token, tm.id)} alt="" width={180} height={180} />
              <code>{tm.voucher}</code>
              <small>{t("Scan to join the team")}</small>
            </div>
          ))}
    </div>
  );
}
