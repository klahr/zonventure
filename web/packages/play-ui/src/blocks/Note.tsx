import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";

/** Text in costume: the style names a look in play-ui.css. Mirror writing reads right only in a mirror. */
export const NoteBlock: Renderer<"note"> = ({ block, ctx }) =>
  block.text ? (
    <div className={`pv-note pv-note--${block.style || "paper"}`}>
      {block.style === "mirror" && <span className="pv-note__caption">{ctx.t("Mirror writing")}</span>}
      <Markdown className="pv-text" text={fillTeam(block.text, ctx)} />
    </div>
  ) : null;

export const DividerBlock: Renderer<"divider"> = ({ block }) =>
  block.style === "ornament" ? (
    <div className="pv-divider pv-divider--ornament" role="separator">
      ❦
    </div>
  ) : (
    <hr className={`pv-divider pv-divider--${block.style || "line"}`} />
  );
