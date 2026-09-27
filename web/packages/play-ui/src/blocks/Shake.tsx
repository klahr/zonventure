import { useEffect, useRef, useState, type CSSProperties } from "react";
import { shakeForce, shakeTarget } from "../gadgets.ts";
import { Markdown } from "../Markdown.tsx";
import { HOLD_START, holdStep, type Hold } from "../sensors.ts";
import { fillTeam, type Renderer } from "./context.ts";
import "./gadgets.css";

type IOSMotion = typeof DeviceMotionEvent & { requestPermission?: () => Promise<"granted" | "denied"> };

/** How long it must keep shaking: a shake, not a bump. */
const HOLD_MS = 500;
/** A device that has sent no reading by then has no motion sensor (a laptop). */
const SILENT_MS = 2000;

type Phase = "idle" | "on" | "done" | "denied" | "none";

/**
 * A shake meter on the phone's accelerometer: shaking past the author's
 * threshold for half a second reveals the text.
 */
export const ShakeBlock: Renderer<"shake"> = ({ block, ctx }) => {
  const { t } = ctx;
  const target = shakeTarget(block.threshold);
  // The meter runs to half as much again as the line, so the line sits two thirds up.
  const full = target * 1.5;
  const [phase, setPhase] = useState<Phase>("idle");
  const [force, setForce] = useState(0);
  const hold = useRef<Hold>(HOLD_START);

  useEffect(() => {
    if (phase !== "on") return;
    let heard = false;
    let shown = 0;
    const onMotion = (e: DeviceMotionEvent) => {
      const a = e.acceleration;
      const g = e.accelerationIncludingGravity;
      const own = a && a.x != null && a.y != null && a.z != null;
      const src = own ? a : g;
      if (!src || src.x == null || src.y == null || src.z == null) return;
      heard = true;
      const now = shakeForce(src.x, src.y, src.z, !own);
      hold.current = holdStep(hold.current, now >= target, performance.now(), HOLD_MS);
      if (hold.current.done) {
        setForce(now);
        setPhase("done");
        return;
      }
      // The needle falls back slowly, as the noise meter's does.
      shown = Math.max(now, shown - 0.6);
      setForce((prev) => (Math.abs(prev - shown) < 0.3 ? prev : shown));
    };
    const silent = setTimeout(() => heard || setPhase("none"), SILENT_MS);
    window.addEventListener("devicemotion", onMotion);
    return () => {
      clearTimeout(silent);
      window.removeEventListener("devicemotion", onMotion);
    };
  }, [phase, target]);

  const start = () => {
    const Motion = typeof DeviceMotionEvent === "undefined" ? undefined : (DeviceMotionEvent as IOSMotion);
    if (!Motion) return setPhase("none");
    hold.current = HOLD_START;
    // iOS asks only from a tap, which this is.
    if (typeof Motion.requestPermission !== "function") return setPhase("on");
    void Motion.requestPermission()
      .then((r) => setPhase(r === "granted" ? "on" : "denied"))
      .catch(() => setPhase("denied"));
  };

  // The designer's preview: shaking, not yet hard enough.
  const shown = ctx.preview ? target * 0.6 : force;
  const level = Math.min(100, (shown / full) * 100);
  const line = (target / full) * 100;
  const done = phase === "done";

  return (
    <figure className={"pv-shake" + (done ? " is-done" : "")}>
      <div
        className={"pv-shake__meter" + (shown >= target ? " is-over" : "")}
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(level)}
        aria-label={t("Shake")}
        style={{ "--level": `${level}%`, "--threshold": `${line}%` } as CSSProperties}
      >
        <span className="pv-shake__fill" />
        <span className="pv-shake__mark" title={t("Hard enough")} />
      </div>
      <figcaption className="pv-gadget__caption">
        {done ? (
          <span className="pv-gadget__cheer">{t("Shaken, not stirred!")}</span>
        ) : phase === "denied" ? (
          <span className="pv-gadget__note">{t("Motion access was turned down. Allow it in the browser's settings and try again.")}</span>
        ) : phase === "none" ? (
          <span className="pv-gadget__note">{t("This phone has no motion sensor, so this one can't be done here.")}</span>
        ) : phase === "on" ? (
          <span className="pv-gadget__hint">{shown >= target ? t("Keep going!") : t("Harder!")}</span>
        ) : (
          <span className="pv-gadget__hint">{t("Hold the phone tight and shake it, past the line.")}</span>
        )}
        {!ctx.preview && (phase === "idle" || phase === "denied") && (
          <button type="button" className="pv-btn pv-btn--primary" onClick={start}>
            {t("Start")}
          </button>
        )}
      </figcaption>
      {done && block.text && <Markdown className="pv-gadget__text" text={fillTeam(block.text, ctx)} />}
    </figure>
  );
};
