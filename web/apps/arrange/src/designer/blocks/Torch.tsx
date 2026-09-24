import { play } from "@zonventure/api";
import { AltInput } from "./Media.tsx";
import type { Editor } from "./types.ts";
import "./pictures.css";

export const TorchEditor: Editor<"torch"> = ({ block, set, t }) => {
  const src = play.mediaURL(block.media);
  return (
    <>
      <div className="pic-ed__stage pic-ed__torch" aria-label={t.t("Preview")} role="img">
        <img className="pic-ed__img pic-ed__dark" src={src} alt="" />
        <img className="pic-ed__img pic-ed__lit" src={src} alt="" />
      </div>
      <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
      <p className="field__hint">{t.t("Teams see the picture in the dark and shine a torch over it with a finger.")}</p>
    </>
  );
};
