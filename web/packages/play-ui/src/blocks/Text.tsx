import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";

export const TextBlock: Renderer<"text"> = ({ block, ctx }) => (block.text ? <Markdown className="pv-text" text={fillTeam(block.text, ctx)} /> : null);

/** The step's title, wherever the author put it. */
export const TitleBlock: Renderer<"title"> = ({ block }) => (block.text ? <h2 className="pv-title">{block.text}</h2> : null);

/** What the puzzle is worth; nothing for a puzzle worth nothing. */
export const PointsBlock: Renderer<"points"> = ({ block, ctx }) =>
  block.points ? <p className="pv-points">{block.points === 1 ? ctx.t("Worth 1 point") : ctx.t("Worth {{n}} points", { n: Number.isInteger(block.points) ? block.points : block.points.toFixed(1) })}</p> : null;
