import { useEffect, useRef, useState, type CSSProperties } from "react";
import type { Translate } from "../feedback.ts";
import { Markdown } from "../Markdown.tsx";
import { HOLD_START, bubble, elevation, holdStep, tilt, type Hold } from "../sensors.ts";
import { fillTeam, type Renderer } from "./context.ts";
import "./level.css";

type IOSOrientation = typeof DeviceOrientationEvent & { requestPermission?: () => Promise<"granted" | "denied"> };

/** How close counts, and for how long. */
const TOLERANCE = 3;
const HOLD_MS = 1500;
/** A device that has sent no reading by then has no tilt sensor (a laptop). */
const SILENT_MS = 2000;

interface Pose {
  beta: number;
  gamma: number;
}

type Phase = "idle" | "on" | "done" | "denied" | "none";

/**
 * A spirit level, or an aim at something above the horizon: holding the
 * phone within 3° of the target for 1.5 s reveals the text.
 */
export const LevelBlock: Renderer<"level"> = ({ block, ctx }) => {
  const { t } = ctx;
  const pitch = block.pitch ?? 0;
  const [phase, setPhase] = useState<Phase>("idle");
  const [pose, setPose] = useState<Pose | null>(null);
  const hold = useRef<Hold>(HOLD_START);

  // A simulated device needs no start and no permission; its pose is
  // checked on a timer, since holding still sends no new readings.
  const motion = ctx.motion;
  const simulated = motion ? { beta: motion.beta, gamma: motion.gamma } : null;
  const simRef = useRef(simulated);
  simRef.current = simulated;
  useEffect(() => {
    if (!motion || phase === "done") return;
    const id = setInterval(() => {
      const p = simRef.current;
      if (!p) return;
      hold.current = holdStep(hold.current, offBy(p, pitch) <= TOLERANCE, performance.now(), HOLD_MS);
      if (hold.current.done) setPhase("done");
    }, 100);
    return () => clearInterval(id);
  }, [!!motion, phase, pitch]);

  useEffect(() => {
    if (phase !== "on" || motion) return;
    let heard = false;
    const onReading = (e: DeviceOrientationEvent) => {
      if (e.beta == null || e.gamma == null) return;
      heard = true;
      const next = { beta: e.beta, gamma: e.gamma };
      hold.current = holdStep(hold.current, offBy(next, pitch) <= TOLERANCE, performance.now(), HOLD_MS);
      if (hold.current.done) setPhase("done");
      // Readings come many times a second; redraw only for a visible move.
      setPose((prev) => (prev && Math.abs(prev.beta - next.beta) < 0.3 && Math.abs(prev.gamma - next.gamma) < 0.3 ? prev : next));
    };
    const silent = setTimeout(() => heard || setPhase("none"), SILENT_MS);
    window.addEventListener("deviceorientation", onReading);
    return () => {
      clearTimeout(silent);
      window.removeEventListener("deviceorientation", onReading);
    };
  }, [phase, pitch, !!motion]);

  const start = () => {
    const Orientation = typeof DeviceOrientationEvent === "undefined" ? undefined : (DeviceOrientationEvent as IOSOrientation);
    if (!Orientation) return setPhase("none");
    hold.current = HOLD_START;
    // iOS asks only from a tap, which this is.
    if (typeof Orientation.requestPermission !== "function") return setPhase("on");
    void Orientation.requestPermission()
      .then((r) => setPhase(r === "granted" ? "on" : "denied"))
      .catch(() => setPhase("denied"));
  };

  // The designer's preview: a little off target, as a player first sees it.
  const shown = simulated ?? (ctx.preview ? (pitch ? { beta: 90 + pitch - 8, gamma: 0 } : { beta: 6, gamma: -4 }) : pose);
  const done = phase === "done";

  return (
    <figure className={"pv-level" + (done ? " is-done" : "")}>
      {pitch ? <Gauge pitch={pitch} pose={shown} t={t} /> : <Vial pose={shown} t={t} />}
      <figcaption className="pv-level__caption">
        {done ? (
          <span className="pv-level__cheer">{t("Steady! You did it.")}</span>
        ) : phase === "none" ? (
          <span className="pv-level__note">{t("This phone has no tilt sensor, so this one can't be done here.")}</span>
        ) : phase === "denied" ? (
          <span className="pv-level__note">{t("Motion access was turned down. Allow it in the browser's settings and try again.")}</span>
        ) : (phase === "on" || simulated) && shown ? (
          <span className="pv-level__hint" role="status">{hint(shown, pitch, t)}</span>
        ) : (
          <span className="pv-level__hint">{pitch ? t("Aim the back camera {{n}}° above the horizon.", { n: pitch }) : t("Hold the phone flat.")}</span>
        )}
        {!ctx.preview && (phase === "idle" || phase === "denied") && (
          <button type="button" className="pv-btn pv-btn--primary" onClick={start}>
            {t("Start")}
          </button>
        )}
      </figcaption>
      {done && block.text && <Markdown className="pv-level__text" text={fillTeam(block.text, ctx)} />}
    </figure>
  );
};

/** Degrees from the target: tilt from flat, or elevation from the aim. */
function offBy(p: Pose, pitch: number): number {
  return pitch ? Math.abs(elevation(p.beta, p.gamma) - pitch) : tilt(p.beta, p.gamma);
}

function hint(p: Pose, pitch: number, t: Translate): string {
  if (offBy(p, pitch) <= TOLERANCE) return t("Hold it there…");
  if (!pitch) return t("Level it out.");
  return elevation(p.beta, p.gamma) < pitch ? t("Aim higher.") : t("Aim lower.");
}

/** Pitch 0: a round vial, the bubble drifting to the high side, the target ring in the middle. */
function Vial({ pose, t }: { pose: Pose | null; t: Translate }) {
  const b = pose ? bubble(pose.beta, pose.gamma) : { x: 0, y: 0 };
  const on = !!pose && tilt(pose.beta, pose.gamma) <= TOLERANCE;
  return (
    <div className={"pv-level__vial" + (on ? " is-on" : "")} role="img" aria-label={pose ? t("Tilted {{n}}°", { n: Math.round(tilt(pose.beta, pose.gamma)) }) : t("Spirit level")}>
      <span className="pv-level__target" />
      {pose && <span className="pv-level__bubble" style={{ "--x": b.x, "--y": b.y } as CSSProperties} />}
    </div>
  );
}

/** Pitch > 0: a quarter dial from the horizon to straight up, the target as a wedge and the aim as a needle. */
function Gauge({ pitch, pose, t }: { pitch: number; pose: Pose | null; t: Translate }) {
  const aim = pose ? elevation(pose.beta, pose.gamma) : null;
  const on = aim != null && Math.abs(aim - pitch) <= TOLERANCE;
  // The pivot sits bottom left; angles run anticlockwise from the horizon.
  const xy = (a: number, r: number) => ({ x: 10 + r * Math.cos((a * Math.PI) / 180), y: 90 - r * Math.sin((a * Math.PI) / 180) });
  const at = (a: number, r: number) => {
    const p = xy(a, r);
    return `${p.x},${p.y}`;
  };
  const tip = aim != null ? xy(Math.max(-8, Math.min(98, aim)), 74) : null;
  const lo = Math.max(0, pitch - TOLERANCE);
  const hi = Math.min(90, pitch + TOLERANCE);
  return (
    <svg
      className={"pv-level__gauge" + (on ? " is-on" : "")}
      viewBox="0 0 100 100"
      role="img"
      aria-label={aim != null ? t("Aiming {{n}}° up, target {{m}}°", { n: Math.round(aim), m: pitch }) : t("Target {{m}}° up", { m: pitch })}
    >
      <path className="pv-level__arc" d={`M ${at(0, 80)} A 80 80 0 0 0 ${at(90, 80)}`} />
      <line className="pv-level__horizon" x1="4" y1="90" x2="96" y2="90" />
      <path className="pv-level__wedge" d={`M 10,90 L ${at(lo, 80)} A 80 80 0 0 0 ${at(hi, 80)} Z`} />
      {tip && <line className="pv-level__needle" x1="10" y1="90" x2={tip.x} y2={tip.y} />}
      <circle className="pv-level__pivot" cx="10" cy="90" r="3" />
    </svg>
  );
}
