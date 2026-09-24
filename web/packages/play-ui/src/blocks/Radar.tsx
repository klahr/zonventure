import type { CSSProperties } from "react";
import { warmthAt, type Band, type Warmth } from "../sensors.ts";
import type { Renderer } from "./context.ts";
import "./radar.css";

const WORD: Record<Band, string> = {
  there: "You're there!",
  burning: "Burning!",
  hot: "Hot",
  warmer: "Warmer",
  cold: "Cold",
  freezing: "Freezing",
};

/** What a designer's preview shows: warm, but not there yet. */
const PREVIEW: Warmth = { band: "warmer", heat: 0.5 };

/**
 * Hot and cold: a pulse that quickens and reddens as the team nears the
 * zone. It never says how far or which way; finding it is the puzzle.
 */
export const RadarBlock: Renderer<"radar"> = ({ block, ctx }) => {
  const { t, position } = ctx;
  if (!block.place) return null;
  const w = ctx.preview ? PREVIEW : position ? warmthAt(position, block.place) : null;
  return (
    <figure className={"pv-radar" + (w ? ` is-${w.band}` : " is-waiting")} style={{ "--heat": w?.heat ?? 0 } as CSSProperties}>
      <div className="pv-radar__scope" aria-hidden="true">
        <span className="pv-radar__ring" />
        <span className="pv-radar__ring" />
        <span className="pv-radar__dot" />
      </div>
      {/* Only the word is announced, so a screen reader hears each change of band once. */}
      <figcaption className="pv-radar__word" role="status">
        {w ? t(WORD[w.band]) : t("Finding your location…")}
      </figcaption>
    </figure>
  );
};
