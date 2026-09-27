import { useEffect, useRef, useState } from "react";
import type { Translate } from "../feedback.ts";
import { headingOff } from "../gadgets.ts";
import { Markdown } from "../Markdown.tsx";
import { HOLD_START, holdStep, type Hold } from "../sensors.ts";
import { useHeading } from "./Compass.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./gadgets.css";

/** How close counts, and for how long. */
const TOLERANCE = 10;
const HOLD_MS = 1000;
/** A device that has sent no heading by then has no compass (a laptop). */
const SILENT_MS = 2500;

type Phase = "idle" | "on" | "done" | "none";

/**
 * A compass to line up: pointing the phone's top edge within 10° of the
 * author's heading and holding it there for a second reveals the text.
 */
export const BearingBlock: Renderer<"bearing"> = ({ block, ctx }) => {
  const { t } = ctx;
  const target = block.heading ?? 0;
  const [phase, setPhase] = useState<Phase>("idle");
  const device = useHeading(!ctx.preview && !ctx.motion);
  const heading = ctx.motion ? ctx.motion.heading : device.heading;
  const hold = useRef<Hold>(HOLD_START);

  // A simulated device needs no start; holding still sends no readings,
  // so the hold is checked on a timer, for both.
  const reading = useRef(heading);
  reading.current = heading;
  const running = phase === "on" || (!!ctx.motion && phase !== "done");
  useEffect(() => {
    if (!running) return;
    const id = setInterval(() => {
      const h = reading.current;
      hold.current = holdStep(hold.current, h != null && headingOff(h, target) <= TOLERANCE, performance.now(), HOLD_MS);
      if (hold.current.done) setPhase("done");
    }, 100);
    return () => clearInterval(id);
  }, [running, target]);

  useEffect(() => {
    if (phase !== "on" || ctx.motion) return;
    const silent = setTimeout(() => reading.current == null && setPhase("none"), SILENT_MS);
    return () => clearTimeout(silent);
  }, [phase, ctx.motion]);

  const start = () => {
    hold.current = HOLD_START;
    // iOS asks only from a tap, which this is.
    device.ask?.();
    setPhase("on");
  };

  // The designer's preview: a quarter turn off, as a player first sees it.
  const shown = ctx.preview ? (target + 70) % 360 : heading;
  const done = phase === "done";

  return (
    <figure className={"pv-bearing" + (done ? " is-done" : "")}>
      <Dial target={target} heading={shown} t={t} />
      <figcaption className="pv-gadget__caption">
        {done ? (
          <span className="pv-gadget__cheer">{t("That's the way!")}</span>
        ) : phase === "none" ? (
          <span className="pv-gadget__note">{t("This phone has no compass, so this one can't be done here.")}</span>
        ) : running && shown != null ? (
          <span className="pv-gadget__hint" role="status">
            {hint(shown, target, t)}
          </span>
        ) : (
          <span className="pv-gadget__hint">{t("Point the top of the phone the right way, and hold it there.")}</span>
        )}
        {!ctx.preview && !ctx.motion && phase === "idle" && (
          <button type="button" className="pv-btn pv-btn--primary" onClick={start}>
            {t("Start")}
          </button>
        )}
      </figcaption>
      {done && block.text && <Markdown className="pv-gadget__text" text={fillTeam(block.text, ctx)} />}
    </figure>
  );
};

function hint(heading: number, target: number, t: Translate): string {
  if (headingOff(heading, target) <= TOLERANCE) return t("Hold it there…");
  const right = (((target - heading) % 360) + 360) % 360 < 180;
  return right ? t("Turn right.") : t("Turn left.");
}

/** A north-up dial: the target as a wedge, the phone's heading as a needle. */
function Dial({ target, heading, t }: { target: number; heading: number | null; t: Translate }) {
  const on = heading != null && headingOff(heading, target) <= TOLERANCE;
  const xy = (a: number, r: number) => {
    const rad = ((a - 90) * Math.PI) / 180;
    return { x: 50 + r * Math.cos(rad), y: 50 + r * Math.sin(rad) };
  };
  const at = (a: number, r: number) => {
    const p = xy(a, r);
    return `${p.x},${p.y}`;
  };
  const tip = heading != null ? xy(heading, 38) : null;
  return (
    <svg
      className={"pv-bearing__dial" + (on ? " is-on" : "")}
      viewBox="0 0 100 100"
      role="img"
      aria-label={heading != null ? t("Pointing {{n}}°", { n: Math.round(heading) }) : t("Compass")}
    >
      <circle className="pv-bearing__ring" cx="50" cy="50" r="44" />
      <path className="pv-bearing__wedge" d={`M 50,50 L ${at(target - TOLERANCE, 44)} A 44 44 0 0 1 ${at(target + TOLERANCE, 44)} Z`} />
      <text className="pv-bearing__north" x="50" y="15" textAnchor="middle">
        N
      </text>
      {tip && <line className="pv-bearing__needle" x1="50" y1="50" x2={tip.x} y2={tip.y} />}
      <circle className="pv-bearing__pivot" cx="50" cy="50" r="3" />
    </svg>
  );
}
