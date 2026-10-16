import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiError } from "@zonventure/api";
import { coverOf } from "@zonventure/play-ui";
import { useConfirm } from "@zonventure/ui";
import { Game, HostedProvider, PersonaliseSection, PlacesSection, PlayerLink, TrailPrint, WhereSection, adaptable, partyApi, type HostedEvent } from "@zonventure/hosted";
import { Resend } from "./Book.tsx";
import { useT } from "./i18n.tsx";
import { useLook } from "./look.ts";
import { holdHostLink, hostEditURL, keepLink, replaceHostLink, setTrailSession, verify } from "./trail.ts";

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
  const [ask, confirmDialog] = useConfirm();
  useLook(party?.look);

  // The mailed link confirms the party's email: opening it is proof the
  // host reads that inbox, so "Get a link by email" finds it later.
  useEffect(() => {
    const m = /^#confirm=(.+)$/.exec(location.hash);
    if (!m) return;
    history.replaceState(null, "", location.pathname);
    verify(decodeURIComponent(m[1]!)).then(setTrailSession, () => {});
  }, []);

  const load = useCallback(() => {
    api.get().then(
      (p) => {
        setParty(p);
        setGone(false);
        if (p.public_trail && p.trail_id) holdHostLink(p.trail_id, token);
      },
      (e) => {
        if (e instanceof ApiError && e.status === 404) setGone(true);
      },
    );
  }, [api, token]);

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
  if (!party) return <p className="zv-muted">{t("Loading…")}</p>;

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
  const cover = coverOf(party.theme, party.cover_url);

  return (
    <HostedProvider value={{ i18n, api, run, busy }}>
      {confirmDialog}
      <div className="party-screen">
        <section className={"party-card party-head" + (cover ? " party-card--cover" : "")}>
          {cover && <div className="pv-cover" style={{ backgroundImage: `url(${cover.wide})` }} aria-hidden="true" />}
          <p className="party-eyebrow">
            {t("Host page")}
            {party.adventure_title !== party.name && " · " + party.adventure_title}
          </p>
          <h1>{party.name}</h1>
          <p className="zv-muted">
            <StatePill state={party.state} />{" "}
            {!party.expired &&
              (party.state === "scheduled"
                ? t("Start your first game by {{date}}.", { date: date(party.valid_until) })
                : party.state === "running" || party.state === "paused"
                  ? t("Play as many games as you like until {{date}}.", { date: date(party.valid_until) })
                  : null)}
          </p>
          {party.expired && party.state === "scheduled" && (
            <p className="zv-note">{t("This party wasn't started in time. Contact {{org}} if you still want to play.", { org: party.org_name })}</p>
          )}
          {party.state === "cancelled" && <p className="zv-note">{t("This party was called off.")}</p>}
          {(party.state === "completed" || (party.expired && party.state !== "scheduled")) && (
            <p className="zv-note">{t("The party is over. The results stay open for a while, then everything about the players is deleted.")}</p>
          )}
        </section>

        {error && (
          <p className="zv-error" role="alert">
            {error}
          </p>
        )}

        {party.round_open && personal && <p className="zv-note">{t("A game is being played. Your own words below can be changed again when it is finished.")}</p>}

        {chooseWhere && (
          <section className="party-card">
            {heading(t("Where are you playing?"))}
            <WhereSection party={party} />
          </section>
        )}

        {party.trail ? (
          <section className="party-card">
            {heading(t("Cards"))}
            {party.public_trail && !party.expired && (party.state === "scheduled" || party.state === "running" || party.state === "paused") && (
              <p>
                <a className="zv-btn" href={hostEditURL(token)}>
                  {t("Edit the party adventure")}
                </a>
              </p>
            )}
            <TrailPrint event={party} />
          </section>
        ) : party.mode === "park" ? (
          <section className="party-card">
            {heading(t("Places"))}
            <PlacesSection party={party} />
          </section>
        ) : (
          <section className="party-card">
            {heading(t("Cards"))}
            {party.stations.length === 0 ? (
              <p className="zv-muted">{t("This adventure has no cards to put out.")}</p>
            ) : (
              <>
                <p className="zv-muted">
                  {party.stations.length === 1
                    ? t("Print the card and put it up where the players will pass: on a tree, a fence or the letterbox.")
                    : t("Print the {{n}} cards and put them out in order along a trail: on a tree, a fence, the letterbox. The game tells each team which card to look for next.", {
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
                <button className="zv-btn zv-btn--primary" onClick={() => setPrinting(true)}>
                  {t("Print the cards")}
                </button>
              </>
            )}
          </section>
        )}

        {personal && (
          <section className="party-card">
            {heading(t("Make it yours"))}
            <p className="zv-muted">{t("The adventure lets you put in your own words. Leave a field as it is to keep the original.")}</p>
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

        {party.public_trail && party.keep_from && new Date(party.keep_from) <= new Date() && (party.state === "running" || party.state === "paused") && !party.expired && (
          <section className="party-card">
            <h2>{t("Keep it")}</h2>
            <p className="zv-muted">
              {t("Your party adventure is deleted on {{date}}. Create a free account and it moves there instead, to play again whenever you like. Moving it ends the games you have now.", {
                date: date(party.valid_until),
              })}
            </p>
            <button type="button" className="zv-btn zv-btn--primary" disabled={busy} onClick={() => void run(async () => (location.href = (await keepLink(token)).url))}>
              {t("Create a free account to keep this party adventure")}
            </button>
          </section>
        )}
        <section className="party-card">
          <h2>{t("The link to this page")}</h2>
          <p className="zv-muted">{t("Anyone with the link can run your party. If it got to someone it shouldn't have, replace it.")}</p>
          <button
            type="button"
            className="zv-btn"
            disabled={busy}
            onClick={async () => {
              if (
                await ask({
                  message: t("Replace the link? Older links stop working, on every phone and in your emails. This page opens with the new link: bookmark it, or have it emailed to you again."),
                  confirmLabel: t("Replace link"),
                  cancelLabel: t("Cancel"),
                  danger: true,
                })
              )
                void run(async () => location.replace("/app/party/host/" + encodeURIComponent((await replaceHostLink(token)).token)));
            }}
          >
            {t("Replace link")}
          </button>
        </section>
        {party.public_trail && (
          <p className="party-right">
            <a href="/app/party/trail/mine">{t("My party adventures")}</a>
          </p>
        )}
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
  return <span className={"zv-pill zv-pill--" + state}>{label}</span>;
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
