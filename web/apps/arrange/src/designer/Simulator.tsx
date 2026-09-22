import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, play } from "@zonventure/api";
import { nodeBySlug, portLabel, type Envelope, type Graph } from "@zonventure/graph";
import { PlayView, StartCeremony, feedbackForRefusal, feedbackForResult, formatSeconds, type Feedback } from "@zonventure/play-ui";
import "@zonventure/play-ui/play-ui.css";
import { useT } from "../i18n.tsx";

/**
 * Preview and test-play, in one.
 *
 * Both show the draft exactly as a team would get it: the screen is
 * @zonventure/play-ui's PlayView, the component the player app renders,
 * and every action runs through the server's simulator, which applies
 * the live engine's rules (internal/graph/session) to the unsaved graph.
 * A right answer here is a right answer in the event; a hint that opens
 * after five minutes here opens after five minutes there.
 *
 * Two modes:
 *
 *   - "step" previews the selected step on its own, as a team arriving
 *     with no history would see it. It follows the selection and every
 *     edit, so the author sees a change as they type it.
 *   - "run" plays the whole adventure from Start. It keeps going across
 *     edits — each action is run against the latest draft — so an author
 *     can fix a step mid-run and carry on.
 *
 * Two controls exist only here: moving the clock, and placing the team
 * in a zone. They are how an author tries a hint delay, a time limit or
 * a zone gate without waiting or walking.
 */

export type SimMode = "step" | "run";

interface Props {
  slug: string;
  graph: Graph;
  envelope: Envelope;
  selected: string | null;
  mode: SimMode;
  onMode: (m: SimMode) => void;
  /** Bumped by the parent to start a run over. */
  restart: number;
  /** Reports where the simulated team is, for the canvas. */
  onPosition: (slug: string | null) => void;
  /** The adventure's palette, as currently edited. */
  brand: play.Brand;
  /** Shown in the start ceremony. */
  title: string;
}

interface LogLine {
  id: number;
  text: string;
  tone?: "warn";
}

/** Steps a player sees. The others run the moment a team reaches them. */
const VISIBLE = new Set(["puzzle", "story", "hub", "end"]);

export function Simulator({ slug, graph, envelope, selected, mode, onMode, restart, onPosition, brand, title }: Props) {
  const t = useT();
  const tr = useCallback((key: string, vars?: Record<string, string | number>) => t.t(key, vars), [t]);

  const [team, setTeam] = useState<play.SimTeam | null>(null);
  const [view, setView] = useState<play.View | null>(null);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const [busy, setBusy] = useState(false);
  const [zone, setZone] = useState("");
  const [log, setLog] = useState<LogLine[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const [ceremony, setCeremony] = useState(false);

  // The latest draft, read at send time so a run in progress uses the
  // graph as it is now rather than as it was when the run started.
  const envRef = useRef(envelope);
  envRef.current = envelope;
  // Read by the preview effect without re-placing the team when the
  // author only moves it.
  const zoneRef = useRef(zone);
  zoneRef.current = zone;
  const logId = useRef(0);

  const say = useCallback((text: string, tone?: "warn") => {
    setLog((l) => [{ id: ++logId.current, text, ...(tone ? { tone } : {}) }, ...l].slice(0, 30));
  }, []);

  const titleOf = useCallback((s: string) => nodeBySlug(graph, s)?.title || s, [graph]);

  const run = useCallback(
    async (action: play.SimAction, from: play.SimTeam | null, signal?: AbortSignal) => {
      setBusy(true);
      setError(null);
      try {
        const r = await play.simulate(slug, envRef.current, from, action, signal);
        if (r.team) setTeam(r.team);
        setView(r.result.view);
        onPosition(r.result.view.node ?? null);
        return r;
      } catch (e) {
        if (signal?.aborted) return null;
        setError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
        return null;
      } finally {
        setBusy(false);
      }
    },
    [slug, onPosition, t],
  );

  // Step mode: place a fresh team on the selected step, again on every
  // edit. Debounced, so typing a prompt does not fire a request per key.
  const placeable = mode === "step" && selected && VISIBLE.has(nodeBySlug(graph, selected)?.type ?? "");
  useEffect(() => {
    if (mode !== "step") return;
    setFeedback(null);
    if (!placeable || !selected) {
      setView(null);
      setTeam(null);
      onPosition(null);
      return;
    }
    const ac = new AbortController();
    const id = setTimeout(() => {
      void run({ kind: "place", node: selected }, null, ac.signal).then((r) => {
        // Keep the team where the author put it, so previewing a
        // zone-gated step does not reset to "not there" on every edit.
        const z = zoneRef.current;
        if (r?.team && z) void run({ kind: "move", zone: z }, r.team, ac.signal);
      });
    }, 300);
    return () => {
      clearTimeout(id);
      ac.abort();
    };
    // envelope is a dependency on purpose: every edit re-places the
    // team, which is what makes this a live preview.
  }, [mode, placeable, selected, envelope, run, onPosition]);

  // Run mode: start from the top when entered or restarted.
  useEffect(() => {
    if (mode !== "run") return;
    setFeedback(null);
    setLog([]);
    setZone("");
    const ac = new AbortController();
    void run({ kind: "start" }, null, ac.signal).then((r) => {
      if (r?.refusal) say(r.refusal.message, "warn");
      else if (r) {
        setCeremony(true);
        say(t.t("Started at {{step}}.", { step: titleOf(r.result.view.node ?? "") }));
      }
    });
    return () => ac.abort();
    // Deliberately not keyed on the graph: a run carries on across edits.
  }, [mode, restart, nonce, run]);

  // Clear the canvas marker when the simulator goes away.
  useEffect(() => () => onPosition(null), [onPosition]);

  const act = async (a: play.PlayAction) => {
    const before = view?.node;
    const r = await run(a, team);
    if (!r) return;
    if (r.refusal) {
      if (r.refusal.code === "graph") {
        setFeedback(null);
        say(t.t("The draft stopped the team: {{why}}", { why: r.refusal.message }), "warn");
      } else {
        setFeedback(feedbackForRefusal(r.refusal.code, tr));
      }
      return;
    }
    setFeedback(feedbackForResult(a.kind, r.result, tr));
    const after = r.result.view.node;
    const left = before ? nodeBySlug(graph, before) : undefined;
    const port = r.result.port && left ? portLabel(left, r.result.port) : r.result.port;
    if (a.kind === "answer") say(t.t("Answered “{{guess}}”.", { guess: a.guess }));
    if (r.result.port) {
      const via = (r.passed ?? []).filter((s) => s !== after).map(titleOf);
      say(
        via.length
          ? t.t("Left by “{{port}}”, through {{via}}, to {{step}}.", { port: port ?? "", via: via.join(" → "), step: titleOf(after ?? "") })
          : t.t("Left by “{{port}}” to {{step}}.", { port: port ?? "", step: titleOf(after ?? "") }),
      );
    } else if (before && after && before !== after) {
      say(t.t("Moved to {{step}}.", { step: titleOf(after) }));
    }
  };

  const wait = (seconds: number) => {
    if (!team) return;
    setFeedback(null);
    void run({ kind: "wait", seconds }, team).then((r) => {
      if (r) say(t.t("Waited {{time}}.", { time: formatSeconds(seconds) }));
    });
  };

  const move = (z: string) => {
    setZone(z);
    if (!team) return;
    setFeedback(null);
    void run(z ? { kind: "move", zone: z } : { kind: "move" }, team).then((r) => {
      if (r) say(z ? t.t("Now standing in {{zone}}.", { zone: zoneName(graph, z) }) : t.t("Now nowhere in particular."));
    });
  };

  const elapsed = team?.now && typeof team["started_at"] === "string"
    ? (Date.parse(team.now) - Date.parse(team["started_at"] as string)) / 1000
    : 0;

  const selectedNode = selected ? nodeBySlug(graph, selected) : undefined;

  return (
    <div className="sim">
      <div className="seg sim__mode" role="group" aria-label={t.t("Preview mode")}>
        <button type="button" className={mode === "step" ? "is-on" : ""} onClick={() => onMode("step")}>
          {t.t("This step")}
        </button>
        <button type="button" className={mode === "run" ? "is-on" : ""} onClick={() => onMode("run")}>
          {t.t("Play from start")}
        </button>
      </div>

      <div className="sim__controls">
        <span className="sim__clock" title={t.t("Time since the team started")}>⏱ {formatSeconds(elapsed)}</span>
        {[60, 300, 1800].map((s) => (
          <button key={s} type="button" className="btn btn--ghost btn--sm" disabled={!team || busy} onClick={() => wait(s)}>
            +{s / 60} {t.t("min")}
          </button>
        ))}
        {mode === "run" && (
          <button type="button" className="btn btn--ghost btn--sm" disabled={busy} onClick={() => setNonce((n) => n + 1)}
            aria-label={t.t("Start over")} title={t.t("Start over")}>
            ↺
          </button>
        )}
      </div>

      <label className="field field--inline sim__where">
        <span className="field__label">{t.t("Team is at")}</span>
        <select className="field__input" value={zone} disabled={!team || busy} onChange={(e) => move(e.target.value)}>
          <option value="">{t.t("Nowhere in particular")}</option>
          {(graph.zones ?? []).map((z) => (
            <option key={z.slug} value={z.slug}>
              {z.name || z.slug}
            </option>
          ))}
        </select>
      </label>

      {error && <div className="alert alert--danger">{error}</div>}

      {/* The frame's transform makes it the containing block for the
          step's fixed overlays — confetti and sheets cover the phone,
          not the designer. */}
      <div className="phone" aria-label={t.t("What the team sees")}>
        <div className="phone__screen">
          {view ? (
            <PlayView view={view} onAction={(a) => void act(a)} busy={busy} feedback={feedback} live={false} t={tr} brand={brand} />
          ) : (
            <p className="phone__empty">
              {mode === "step" && selectedNode && !VISIBLE.has(selectedNode.type)
                ? t.t("Teams never see this step. It happens the moment they reach it, and they carry on.")
                : t.t("Select a puzzle, story or hub to see it as a team will.")}
            </p>
          )}
        </div>
        {ceremony && <StartCeremony title={title} brand={brand} onDone={() => setCeremony(false)} />}
      </div>

      {log.length > 0 && (
        <ol className="sim__log" aria-label={t.t("What happened")}>
          {log.map((l) => (
            <li key={l.id} className={l.tone === "warn" ? "is-warn" : ""}>
              {l.text}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function zoneName(g: Graph, slug: string) {
  return g.zones?.find((z) => z.slug === slug)?.name || slug;
}
