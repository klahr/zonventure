import { play } from "@zonventure/api";
import { AltInput } from "./Media.tsx";
import type { Editor } from "./types.ts";

export const OverlayEditor: Editor<"overlay"> = ({ block, set, t }) => (
  <>
    <div className="media-ed__item">
      <img className="media-ed__thumb" src={play.mediaURL(block.media)} alt="" />
      <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
    </div>
    <p className="field__hint">{t.t("Teams see this picture over their camera, see-through, to line it up with the street.")}</p>
  </>
);
