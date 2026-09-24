import { formatSeconds } from "../clock.ts";
import { fillTeam, type Renderer } from "./context.ts";

/** Counts down on the engine's clock to the moment the server worked out. */
export const CountdownBlock: Renderer<"countdown"> = ({ block, ctx }) => {
  if (!block.ends_at) return null;
  const left = Math.max(0, Math.round((Date.parse(block.ends_at) - ctx.now) / 1000));
  return (
    <div className={"pv-countdown" + (left === 0 ? " is-over" : left < 60 ? " is-urgent" : "")} role="timer">
      {block.text && <span className="pv-countdown__label">{fillTeam(block.text, ctx)}</span>}
      <span className="pv-countdown__time">{left === 0 ? ctx.t("Time is up.") : formatSeconds(left)}</span>
    </div>
  );
};
