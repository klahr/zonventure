import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiError } from "@zonventure/api";
import { Game, HostedProvider, PersonaliseSection, PlacesSection, PlayerLink, WhereSection, adaptable, partyApi, type HostedEvent } from "@zonventure/hosted";
import { Resend } from "./Book.tsx";
import { useT } from "./i18n.tsx";

/**
 * The host page: a hosted event's player link, its cards to print and
 * hide, what the host may make their own, and the games — each started
 * by the first team to join, and finished here. The link is the host's
 * only key.
 */
export function Host({ token }: { token: string }) {
  const i18n = useT();
  const t = i18n.t;
  const api = useMemo(() => partyApi(token), [token]);
  const [party, setParty] = useState<HostedEvent | null>(null);
  const [gone, setGone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [printing, setPrinting] = useState(false);

  const load = useCallback(() => {
    api.get().then(
      (p) => {
        setParty(p);
        setGone(false);
      },
      (e) => {
        if (e instanceof ApiError && e.status === 404) setGone(true);
      },
    );
  }, [api]);

  useEffect(load, [load]);
  // Who has joined and how far each team got: often during a game.
  const state = party?.state;
  const roundOpen = party?.round_open;
  useEffect(() => {
    if (!state || state === "completed" || state === "cancelled") return;
    const id = setInterval(load, roundOpen ? 8_000 : 20_000);
    return () => clearInterval(id);
  }, [state, roundOpen, load]);

  // Print the cards once they are on the page with their QR codes
  // drawn, then put them away.
  useEffect(() => {
    if (!printing) return;
    let cancelled = false;
    const done = () => setPrinting(false);
    window.addEventListener("afterprint", done, { once: true });
    const images = Array.from(document.querySelectorAll<HTMLImageElement>(".party-print img"));
    void Promise.all(images.map((img) => img.decode().catch(() => undefined))).then(() => {
      if (!cancelled) window.print();
    });
    return () => {
      cancelled = true;
      window.removeEventListener("afterprint", done);
    };
  }, [printing]);

  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
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
    },
    [load, t],
  );

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

  const date = (s: string) => new Date(s).toLocaleDateString(i18n.current(), { day: "numeric", month: "long" });
  // The steps a host goes through, numbered by what this party needs.
  let step = 0;
  const heading = (title: string) => (
    <h2>
      <span className="party-step">{++step}</span> {title}
    </h2>
  );
  const chooseWhere = adaptable(party) && party.cards_ok && party.park_ok;
  const personal = party.values.length > 0 || party.editables.length > 0;

  return (
    <HostedProvider value={{ i18n, api, run, busy }}>
      <div className="party-screen">
        <section className="party-card party-head">
          <p className="party-eyebrow">{party.adventure_title}</p>
          <h1>{party.name}</h1>
          <p className="party-muted">
            <StatePill state={party.state} />{" "}
            {!party.expired &&
              (party.state === "scheduled"
                ? t("Start your first game by {{date}}.", { date: date(party.valid_until) })
                : party.state === "running" || party.state === "paused"
                  ? t("Play as many games as you like until {{date}}.", { date: date(party.valid_until) })
                  : null)}
          </p>
          {party.expired && party.state === "scheduled" && (
            <p className="party-note">{t("This party wasn't started in time. Contact {{org}} if you still want to play.", { org: party.org_name })}</p>
          )}
          {party.state === "cancelled" && <p className="party-note">{t("This party was called off.")}</p>}
          {(party.state === "completed" || (party.expired && party.state !== "scheduled")) && (
            <p className="party-note">{t("The party is over. The results stay open for a while, then everything about the players is deleted.")}</p>
          )}
        </section>

        {error && (
          <p className="party-error" role="alert">
            {error}
          </p>
        )}

        {party.round_open && <p className="party-note">{t("A game is being played. What you set up below can be changed again when it is finished.")}</p>}

        {chooseWhere && (
          <section className="party-card">
            {heading(t("Where are you playing?"))}
            <WhereSection party={party} />
          </section>
        )}

        {party.mode === "park" ? (
          <section className="party-card">
            {heading(t("Places"))}
            <PlacesSection party={party} />
          </section>
        ) : (
          <section className="party-card">
            {heading(t("Cards"))}
            {party.stations.length === 0 ? (
              <p className="party-muted">{t("This adventure has no cards to hide.")}</p>
            ) : (
              <>
                <p className="party-muted">
                  {t("Print the {{n}} cards and hide them where the players can find them: under a cushion, behind a tree, in the letterbox. The game tells each team which card to look for next.", {
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
                <button className="party-btn party-btn--primary" onClick={() => setPrinting(true)}>
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
            <PersonaliseSection party={party} />
          </section>
        )}

        <section className="party-card">
          {heading(t("Let the teams join"))}
          <PlayerLink event={party} />
        </section>

        <section className="party-card">
          {heading(t("Play"))}
          <Game event={party} />
        </section>
      </div>

      {printing && <PrintSheet party={party} qr={api.qr} />}
    </HostedProvider>
  );
}

function StatePill({ state }: { state: HostedEvent["state"] }) {
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

/** What the printer gets: the player link to put up, and the cards to hide. */
function PrintSheet({ party, qr }: { party: HostedEvent; qr: (station?: string) => string }) {
  const t = useT().t;
  return (
    <div className="party-print">
      <div className="party-print__card">
        <strong>{party.name}</strong>
        <img src={qr()} alt="" width={180} height={180} />
        <small>{t("Scan to join the game")}</small>
      </div>
      {party.stations.map((s, i) => (
        <div className="party-print__card" key={s.zone}>
          <span className="party-print__no">{i + 1}</span>
          <strong>{s.name}</strong>
          <img src={qr(s.zone)} alt="" width={180} height={180} />
          <code>{s.code}</code>
          <small>{t("Scan me in the game")}</small>
        </div>
      ))}
    </div>
  );
}
