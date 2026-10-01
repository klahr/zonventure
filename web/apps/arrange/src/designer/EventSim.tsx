import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, play } from "@zonventure/api";
import { nodeBySlug, type Envelope, type Graph } from "@zonventure/graph";
import { formatSeconds } from "@zonventure/play-ui";
import { Pause, Play, RotateCcw, Shuffle } from "lucide-react";
import { useT } from "../i18n.tsx";
import { STYLE, loadMapLibre, shape } from "./ZoneMap.tsx";
import { conceptColor } from "./dz/concepts.tsx";
import { ICON } from "./dz/ui.tsx";
import { DEFAULT_CENTRE } from "./zones.ts";

/**
 * A whole event, played by bot teams on the draft.
 *
 * The server (internal/sim) runs the event in one go on the live rules —
 * teams walking at their own pace, guessing wrong, waiting for hints,
 * nudged by a simulated arranger — and hands back the timeline. This
 * plays it back on a map at whatever speed the author likes, with the
 * standings and what happened as it happened, and sums up how each step
 * went: where teams spend their time, guess wrong and need help.
 */

interface Props {
  slug: string;
  graph: Graph;
  envelope: Envelope;
}

const SPEEDS = [1, 10, 30, 60, 120, 300, 600, 1800] as const;
const SPREADS = [
  { value: 0.2, label: "Much alike" },
  { value: 0.6, label: "Mixed" },
  { value: 1, label: "Very mixed" },
] as const;

const SETTINGS_KEY = "zonventure.eventsim";

interface Settings {
  teams: number;
  members: number;
  spread: number;
  /** Minutes between the arranger's messages; 0 for none. */
  every: number;
  /** Hours the run may last. */
  hours: number;
  /** Minutes over which teams begin. */
  stagger: number;
  /** "HH:MM", today: when the event starts. */
  at: string;
  seed: number;
}

const DEFAULTS: Settings = { teams: 20, members: 4, spread: 0.6, every: 10, hours: 4, stagger: 5, at: "10:00", seed: 1 };

function loadSettings(): Settings {
  try {
    const v = JSON.parse(localStorage.getItem(SETTINGS_KEY) ?? "null") as Partial<Settings> | null;
    return { ...DEFAULTS, ...(v ?? {}) };
  } catch {
    return DEFAULTS;
  }
}

function saveSettings(s: Settings) {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  } catch {
    /* private mode: remembered for this visit only */
  }
}

/** A team's colour, spread round the wheel so neighbours differ. */
const colour = (i: number) => `hsl(${(i * 137.5) % 360} 70% 45%)`;

/** Where a team is at t, between the waypoints either side. */
function positionAt(path: play.EventSimTeam["path"], t: number): { lat: number; lng: number } | null {
  if (!path || path.length === 0) return null;
  if (t <= path[0]!.t) return path[0]!;
  let lo = 0;
  let hi = path.length - 1;
  if (t >= path[hi]!.t) return path[hi]!;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (path[mid]!.t <= t) lo = mid;
    else hi = mid;
  }
  const a = path[lo]!;
  const b = path[hi]!;
  const f = b.t === a.t ? 1 : (t - a.t) / (b.t - a.t);
  return { lat: a.lat + (b.lat - a.lat) * f, lng: a.lng + (b.lng - a.lng) * f };
}

/** Score and steps done at t. */
function progressAt(team: play.EventSimTeam, t: number) {
  let score = 0;
  let steps = 0;
  for (const m of team.progress ?? []) {
    if (m.t > t) break;
    score = m.score;
    steps = m.steps;
  }
  return { score, steps };
}

export function EventSim({ slug, graph, envelope }: Props) {
  const t = useT();
  const [settings, setSettings] = useState<Settings>(loadSettings);
  const [result, setResult] = useState<play.EventSimResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [clock, setClock] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<number>(60);
  const [chosen, setChosen] = useState<number | null>(null);

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) =>
    setSettings((s) => {
      const next = { ...s, [k]: v };
      saveSettings(next);
      return next;
    });

  const run = useCallback(async () => {
    setBusy(true);
    setError(null);
    setPlaying(false);
    const [hh, mm] = settings.at.split(":").map(Number);
    const start = new Date();
    start.setHours(hh || 0, mm || 0, 0, 0);
    try {
      const r = await play.simulateEvent(slug, envelope, {
        teams: settings.teams,
        members: settings.members,
        spread: settings.spread,
        message_every: Math.round(settings.every * 60),
        max_seconds: Math.round(settings.hours * 3600),
        start_spread: Math.round(settings.stagger * 60),
        start: start.toISOString(),
        seed: settings.seed,
      });
      setResult(r);
      setClock(0);
      setChosen(null);
      setPlaying(true);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
    } finally {
      setBusy(false);
    }
  }, [slug, envelope, settings, t]);

  // The clock: speed seconds of event per second of playback.
  useEffect(() => {
    if (!playing || !result) return;
    let last = performance.now();
    let id = requestAnimationFrame(function tick(now) {
      const dt = (now - last) / 1000;
      last = now;
      setClock((c) => {
        const next = Math.min(result.duration, c + dt * speed);
        if (next >= result.duration) setPlaying(false);
        return next;
      });
      id = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(id);
  }, [playing, speed, result]);

  const titleOf = useCallback((s?: string) => (s ? nodeBySlug(graph, s)?.title || s : ""), [graph]);

  return (
    <div className="esim">
      <div className="esim__side">
        <div className="esim__form">
          <label className="field">
            <span className="field__label">{t.t("Teams")}</span>
            <input className="field__input" type="number" min={1} max={200} value={settings.teams} onChange={(e) => set("teams", Number(e.target.value))} />
          </label>
          <label className="field">
            <span className="field__label">{t.t("Players per team")}</span>
            <input className="field__input" type="number" min={1} max={12} value={settings.members} onChange={(e) => set("members", Number(e.target.value))} />
          </label>
          <label className="field">
            <span className="field__label">{t.t("Teams are")}</span>
            <select className="field__input" value={settings.spread} onChange={(e) => set("spread", Number(e.target.value))}>
              {SPREADS.map((s) => (
                <option key={s.value} value={s.value}>
                  {t.t(s.label)}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t.t("Arranger writes every")}</span>
            <select className="field__input" value={settings.every} onChange={(e) => set("every", Number(e.target.value))}>
              <option value={0}>{t.t("Never")}</option>
              {[2, 5, 10, 15, 30].map((m) => (
                <option key={m} value={m}>
                  {t.t("{{n}} min", { n: m })}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t.t("Starts at")}</span>
            <input className="field__input" type="time" value={settings.at} onChange={(e) => set("at", e.target.value)} />
          </label>
          <label className="field">
            <span className="field__label">{t.t("Teams begin within (min)")}</span>
            <input className="field__input" type="number" min={0} max={120} value={settings.stagger} onChange={(e) => set("stagger", Number(e.target.value))} />
          </label>
          <label className="field">
            <span className="field__label">{t.t("Stop after (hours)")}</span>
            <input className="field__input" type="number" min={0.5} max={24} step={0.5} value={settings.hours} onChange={(e) => set("hours", Number(e.target.value))} />
          </label>
          <label className="field">
            <span className="field__label">{t.t("Seed")}</span>
            <span className="esim__seed">
              <input className="field__input" type="number" min={0} value={settings.seed} onChange={(e) => set("seed", Number(e.target.value))} />
              <button type="button" className="btn btn--ghost btn--sm" title={t.t("Another run")} aria-label={t.t("Another run")} onClick={() => set("seed", Math.floor(Math.random() * 1e6))}>
                <Shuffle size={ICON.sm} />
              </button>
            </span>
          </label>
        </div>
        <span className="field__hint">{t.t("The same seed plays the same event again, so a fix can be compared with what went before.")}</span>
        <button type="button" className="btn btn--primary" disabled={busy} onClick={() => void run()}>
          {busy ? t.t("Simulating…") : t.t("Simulate event")}
        </button>
        {error && <div className="alert alert--danger">{error}</div>}
        {result && <Summary result={result} />}
      </div>

      <div className="esim__main">
        {result ? (
          <>
            <div className="esim__controls">
              <button type="button" className="btn btn--ghost btn--sm" aria-label={playing ? t.t("Pause") : t.t("Play")} onClick={() => (clock >= result.duration ? (setClock(0), setPlaying(true)) : setPlaying((p) => !p))}>
                {playing ? <Pause size={ICON.sm} /> : clock >= result.duration ? <RotateCcw size={ICON.sm} /> : <Play size={ICON.sm} />}
              </button>
              <span className="esim__clock" title={t.t("Time since the event started")}>
                ⏱ {formatSeconds(clock)} · {new Date(new Date(result.start).getTime() + clock * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
              </span>
              <input className="esim__scrub" type="range" min={0} max={result.duration} step={1} value={clock} aria-label={t.t("Event time")} onChange={(e) => setClock(Number(e.target.value))} />
              <label className="esim__speed">
                <span className="field__label">{t.t("Speed")}</span>
                <select className="field__input" value={speed} onChange={(e) => setSpeed(Number(e.target.value))}>
                  {SPEEDS.map((s) => (
                    <option key={s} value={s}>
                      {s}×
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <TeamMap graph={graph} result={result} clock={clock} chosen={chosen} />
            <div className="esim__panes">
              <Standings result={result} clock={clock} chosen={chosen} onChoose={setChosen} titleOf={titleOf} />
              <Feed result={result} clock={clock} titleOf={titleOf} />
            </div>
            <StepTable result={result} titleOf={titleOf} />
          </>
        ) : (
          <p className="field__hint esim__empty">{t.t("Bot teams play the draft as an event: each at its own pace and skill, taking wrong turns and wrong guesses, waiting for hints and taking them. Set it up and simulate, then watch it at any speed.")}</p>
        )}
      </div>
    </div>
  );
}

function Summary({ result }: { result: play.EventSimResult }) {
  const t = useT();
  const done = result.teams.filter((x) => x.finished != null);
  const times = done.map((x) => x.finished!).sort((a, b) => a - b);
  const sum = (f: (x: play.EventSimTeam) => number) => result.teams.reduce((s, x) => s + f(x), 0);
  const ended = { finished: t.t("Every team finished."), time: t.t("Stopped at the time limit."), stalled: t.t("No team could go on.") }[result.ended];
  return (
    <dl className="esim__summary">
      <div>
        <dt>{t.t("Finished")}</dt>
        <dd>
          {done.length} / {result.teams.length}
        </dd>
      </div>
      {times.length > 0 && (
        <div>
          <dt>{t.t("Fastest · median · slowest")}</dt>
          <dd>
            {formatSeconds(times[0]!)} · {formatSeconds(times[times.length >> 1]!)} · {formatSeconds(times[times.length - 1]!)}
          </dd>
        </div>
      )}
      <div>
        <dt>{t.t("Wrong answers")}</dt>
        <dd>{sum((x) => x.wrong)}</dd>
      </div>
      <div>
        <dt>{t.t("Hints taken")}</dt>
        <dd>{sum((x) => x.hints)}</dd>
      </div>
      <div>
        <dt>{t.t("Skipped")}</dt>
        <dd>{sum((x) => x.skips)}</dd>
      </div>
      <div>
        <dt>{t.t("Nudges")}</dt>
        <dd>{sum((x) => x.nudges)}</dd>
      </div>
      <div>
        <dt>{t.t("Stuck")}</dt>
        <dd>{result.teams.filter((x) => x.stalled).length}</dd>
      </div>
      <p className="field__hint">{ended}</p>
    </dl>
  );
}

type Feature = { type: "Feature"; properties: Record<string, unknown>; geometry: { type: "Point"; coordinates: number[] } | { type: "LineString"; coordinates: number[][] } };

function TeamMap({ graph, result, clock, chosen }: { graph: Graph; result: play.EventSimResult; clock: number; chosen: number | null }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const mapRef = useRef<import("maplibre-gl").Map | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const zones = graph.zones ?? [];

  useEffect(() => {
    let cancelled = false;
    loadMapLibre()
      .then((lib) => {
        if (cancelled || !box.current) return;
        const first = zones[0] ?? DEFAULT_CENTRE;
        const map = new lib.Map({ container: box.current, style: STYLE, center: [first.lng, first.lat], zoom: 14, attributionControl: { compact: true } });
        map.addControl(new lib.NavigationControl({ showCompass: false }), "top-right");
        map.on("load", () => {
          const empty = { type: "FeatureCollection" as const, features: [] };
          map.addSource("zones", { type: "geojson", data: empty });
          map.addLayer({ id: "zones-fill", type: "fill", source: "zones", paint: { "fill-color": conceptColor("place", "#1b6b43"), "fill-opacity": 0.15 } });
          map.addLayer({ id: "zones-line", type: "line", source: "zones", paint: { "line-color": conceptColor("place", "#1b6b43"), "line-width": 1.5 } });
          map.addSource("trail", { type: "geojson", data: empty });
          map.addLayer({ id: "trail", type: "line", source: "trail", paint: { "line-color": ["get", "colour"], "line-width": 2, "line-opacity": 0.6 } });
          map.addSource("teams", { type: "geojson", data: empty });
          map.addLayer({
            id: "members",
            type: "circle",
            source: "teams",
            filter: ["==", ["get", "member"], true],
            paint: { "circle-radius": 3, "circle-color": ["get", "colour"], "circle-opacity": 0.7 },
          });
          map.addLayer({
            id: "teams",
            type: "circle",
            source: "teams",
            filter: ["==", ["get", "member"], false],
            paint: {
              "circle-radius": ["case", ["get", "chosen"], 9, 6],
              "circle-color": ["get", "colour"],
              "circle-stroke-color": "#fff",
              "circle-stroke-width": ["case", ["get", "finished"], 0, 2],
              "circle-opacity": ["case", ["get", "finished"], 0.4, 1],
            },
          });
          setReady(true);
        });
        mapRef.current = map;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      mapRef.current?.remove();
      mapRef.current = null;
    };
    // Created once; the layers follow through the effects below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const src = mapRef.current?.getSource("zones") as import("maplibre-gl").GeoJSONSource | undefined;
    src?.setData({ type: "FeatureCollection", features: zones.map(shape) });
  }, [zones, ready]);

  // Frame every team's walk once per run.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    const pts = result.teams.flatMap((x) => x.path ?? []);
    if (pts.length === 0) return;
    const lats = pts.map((p) => p.lat);
    const lngs = pts.map((p) => p.lng);
    map.fitBounds(
      [
        [Math.min(...lngs), Math.min(...lats)],
        [Math.max(...lngs), Math.max(...lats)],
      ],
      { padding: 30, maxZoom: 17, duration: 0 },
    );
  }, [result, ready]);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    const features: Feature[] = [];
    const trail: Feature[] = [];
    result.teams.forEach((team, i) => {
      const p = positionAt(team.path, clock);
      if (!p) return;
      const finished = team.finished != null && team.finished <= clock;
      const c = colour(i);
      features.push({ type: "Feature", properties: { colour: c, member: false, chosen: chosen === i, finished }, geometry: { type: "Point", coordinates: [p.lng, p.lat] } });
      // Players stay close to their team, a few metres apart.
      for (let m = 0; m < team.members; m++) {
        const a = (m / team.members) * 2 * Math.PI + i;
        const d = 0.00004 + 0.00002 * Math.sin(clock / 40 + m);
        features.push({ type: "Feature", properties: { colour: c, member: true }, geometry: { type: "Point", coordinates: [p.lng + d * Math.cos(a) * 1.8, p.lat + d * Math.sin(a)] } });
      }
      if (chosen === i) {
        const walked = (team.path ?? []).filter((w) => w.t <= clock).map((w) => [w.lng, w.lat]);
        trail.push({ type: "Feature", properties: { colour: c }, geometry: { type: "LineString", coordinates: [...walked, [p.lng, p.lat]] } });
      }
    });
    (map.getSource("teams") as import("maplibre-gl").GeoJSONSource | undefined)?.setData({ type: "FeatureCollection", features });
    (map.getSource("trail") as import("maplibre-gl").GeoJSONSource | undefined)?.setData({ type: "FeatureCollection", features: trail });
  }, [result, clock, chosen, ready]);

  if (failed) return <p className="field__hint">{t.t("The map could not load. Check the connection.")}</p>;
  if (zones.length === 0) return <p className="field__hint">{t.t("The adventure has no places, so there is nothing to show on a map.")}</p>;
  return <div className="esim__map" ref={box} />;
}

function Standings({
  result,
  clock,
  chosen,
  onChoose,
  titleOf,
}: {
  result: play.EventSimResult;
  clock: number;
  chosen: number | null;
  onChoose: (i: number | null) => void;
  titleOf: (s?: string) => string;
}) {
  const t = useT();
  // Where each team is now: the step it last arrived at, from the feed.
  const at = useMemo(() => {
    const out = new Map<number, string>();
    for (const e of result.events ?? []) {
      if (e.t > clock) break;
      if (e.node && e.team >= 0) out.set(e.team, e.node);
    }
    return out;
  }, [result, clock]);
  const rows = result.teams
    .map((team, i) => ({ team, i, ...progressAt(team, clock), finished: team.finished != null && team.finished <= clock }))
    .sort((a, b) => Number(b.finished) - Number(a.finished) || b.score - a.score || b.steps - a.steps || (a.team.finished ?? 0) - (b.team.finished ?? 0));
  return (
    <div className="esim__pane">
      <h4>{t.t("Standings")}</h4>
      <ol className="esim__standings">
        {rows.map((r) => (
          <li key={r.i}>
            <button type="button" className={"esim__team" + (chosen === r.i ? " is-on" : "")} onClick={() => onChoose(chosen === r.i ? null : r.i)} title={t.t("Pace {{pace}} m/s · skill {{skill}} · patience {{patience}}", { pace: r.team.pace, skill: r.team.skill, patience: r.team.patience })}>
              <span className="esim__dot" style={{ background: colour(r.i) }} />
              <span className="esim__name">{r.team.name}</span>
              <span className="esim__where">{r.finished ? t.t("Finished") : r.team.stalled && !at.has(r.i) ? t.t("Stuck") : titleOf(at.get(r.i))}</span>
              <span className="esim__score">{Math.round(r.score)}</span>
            </button>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Feed({ result, clock, titleOf }: { result: play.EventSimResult; clock: number; titleOf: (s?: string) => string }) {
  const t = useT();
  const lines = useMemo(() => {
    const evs = result.events ?? [];
    let n = 0;
    while (n < evs.length && evs[n]!.t <= clock) n++;
    return evs.slice(Math.max(0, n - 60), n).reverse();
  }, [result, clock]);

  const say = (e: play.EventSimEvent): string | null => {
    const team = e.team >= 0 ? (result.teams[e.team]?.name ?? "") : "";
    const step = titleOf(e.node);
    switch (e.kind) {
      case "start":
        return t.t("{{team}} began.", { team });
      case "arrive":
        return t.t("{{team}} reached {{place}}.", { team, place: e.text ?? "" });
      case "astray":
        return t.t("{{team}} took a wrong turn.", { team });
      case "split":
        return t.t("{{team}} split up to cover {{places}}.", { team, places: e.text ?? "" });
      case "wrong":
        return t.t("{{team}} guessed “{{guess}}” at {{step}}.", { team, guess: e.text ?? "", step });
      case "right":
        return t.t("{{team}} solved {{step}}.", { team, step });
      case "locked":
        return t.t("{{team}} is locked out at {{step}}.", { team, step });
      case "hint":
        return t.t("{{team}} took hint {{n}} at {{step}}.", { team, n: e.text ?? "", step });
      case "skip":
        return t.t("{{team}} skipped {{step}}.", { team, step });
      case "timeout":
        return t.t("{{team}} ran out of time at {{step}}.", { team, step });
      case "timeup":
        return t.t("{{team}}’s time is up.", { team });
      case "choose":
        return t.t("{{team}} chose “{{port}}” at {{step}}.", { team, port: e.text ?? "", step });
      case "back":
        return t.t("{{team}} went back from {{step}}.", { team, step });
      case "leave":
        return t.t("{{team}} left {{step}} to find what it needs.", { team, step });
      case "continue":
        return t.t("{{team}} moved on from {{step}}.", { team, step });
      case "pick":
        return t.t("{{team}} answered a question at {{step}}.", { team, step });
      case "language":
        return t.t("{{team}} switched language to {{lang}}.", { team, lang: e.text ?? "" });
      case "message":
        return e.team < 0 ? t.t("The arranger wrote to every team.") : t.t("The arranger nudged {{team}} at {{step}}.", { team, step });
      case "nudge":
        return t.t("{{team}} acted on the nudge.", { team });
      case "forced":
        return t.t("{{team}} passed {{step}}: its answer is a pattern a bot cannot type.", { team, step });
      case "stalled":
        return t.t("{{team}} is stuck at {{step}}: {{why}}", { team, step, why: e.text ?? "" });
      case "finish":
        return t.t("{{team}} finished.", { team });
    }
    return null;
  };

  return (
    <div className="esim__pane">
      <h4>{t.t("What happened")}</h4>
      <ol className="esim__feed">
        {lines.map((e, i) => {
          const text = say(e);
          if (!text) return null;
          return (
            <li key={`${e.t}-${i}`} className={e.kind === "stalled" ? "is-warn" : e.kind === "message" || e.kind === "nudge" ? "is-note" : ""}>
              <span className="esim__at">{formatSeconds(e.t)}</span> {text}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function StepTable({ result, titleOf }: { result: play.EventSimResult; titleOf: (s?: string) => string }) {
  const t = useT();
  const steps = (result.steps ?? []).filter((s) => s.type === "puzzle" || s.type === "story" || s.type === "hub");
  if (steps.length === 0) return null;
  // The step teams spend longest on is the one to look at first.
  const slowest = Math.max(...steps.map((s) => s.median));
  return (
    <div className="esim__pane">
      <h4>{t.t("Steps")}</h4>
      <div className="esim__tablewrap">
        <table className="esim__table">
          <thead>
            <tr>
              <th>{t.t("Step")}</th>
              <th>{t.t("Reached")}</th>
              <th title={t.t("Including the walk there")}>{t.t("Median time")}</th>
              <th>{t.t("Longest")}</th>
              <th>{t.t("Wrong")}</th>
              <th>{t.t("Hints")}</th>
              <th>{t.t("Skips")}</th>
              <th>{t.t("Nudges")}</th>
              <th>{t.t("Still there")}</th>
            </tr>
          </thead>
          <tbody>
            {steps.map((s) => (
              <tr key={s.node} className={s.median === slowest && slowest > 0 ? "is-slow" : ""}>
                <td>
                  {titleOf(s.node)}
                  {s.forced > 0 && (
                    <span className="field__hint" title={t.t("Bots cannot type an answer that is a pattern; they passed it without one.")}>
                      {" "}
                      ({t.t("passed {{n}}", { n: s.forced })})
                    </span>
                  )}
                </td>
                <td>{s.reached}</td>
                <td>{formatSeconds(s.median)}</td>
                <td>{formatSeconds(s.max)}</td>
                <td>{s.wrong}</td>
                <td>{s.hints}</td>
                <td>{s.skips + s.timeouts}</td>
                <td>{s.nudges}</td>
                <td>{s.waiting}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
