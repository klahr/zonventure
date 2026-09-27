import { play } from "@zonventure/api";
import type { BlockKind } from "@zonventure/graph";
import type { Editor, Of, T } from "./types.ts";
import { withField } from "./types.ts";

/** The description of a block's file, for someone who cannot see or hear it. */
export function AltInput<B extends Of<"image" | "audio" | "video" | "hotspots" | "overlay" | "torch" | "call" | "spotdiff">>({ block, set, placeholder, t }: { block: B; set: (b: B) => void; placeholder: string; t: T }) {
  return <input className="field__input" value={block.alt ?? ""} placeholder={placeholder} aria-label={t.t("Description")} onChange={(e) => set(withField(block, "alt", e.target.value) as B)} />;
}

export const ImageEditor: Editor<"image"> = ({ block, set, t }) => (
  <div className="media-ed__item">
    <img className="media-ed__thumb" src={play.mediaURL(block.media)} alt="" />
    <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
  </div>
);

export const AudioEditor: Editor<"audio"> = ({ block, set, t }) => (
  <>
    <div className="media-ed__item">
      <audio className="media-ed__audio" controls preload="none" src={play.mediaURL(block.media)} />
      <AltInput block={block} set={set} placeholder={t.t("Describe the sound")} t={t} />
    </div>
    <label className="check">
      <input type="checkbox" checked={!!block.reverse} onChange={(e) => set(withField(block, "reverse", e.target.checked))} />
      <span>{t.t("Can be played backwards")}</span>
    </label>
    <label className="check">
      <input type="checkbox" checked={!!block.spectrogram} onChange={(e) => set(withField(block, "spectrogram", e.target.checked))} />
      <span>{t.t("Show its spectrogram")}</span>
    </label>
    {(block.reverse || block.spectrogram) && (
      <p className="field__hint">{t.t("A sound with these shows on its own, with its own player, not in a row with the pictures beside it. A spectrogram shows a picture drawn into the sound.")}</p>
    )}
  </>
);

export const CallEditor: Editor<"call"> = ({ block, set, t }) => (
  <>
    <input className="field__input" value={block.who ?? ""} placeholder={t.t("Such as “Anna”")} aria-label={t.t("Caller")} onChange={(e) => set(withField(block, "who", e.target.value))} />
    <div className="media-ed__item">
      <audio className="media-ed__audio" controls preload="none" src={play.mediaURL(block.media)} />
      <AltInput block={block} set={set} placeholder={t.t("What the caller says, for someone who cannot hear it")} t={t} />
    </div>
    <p className="field__hint">{t.t("The phone rings with the caller's name; answering plays the sound, hanging up lets the team call back.")}</p>
  </>
);

export const VideoEditor: Editor<"video"> = ({ block, set, t }) => (
  <>
    <video className="blk__video" controls preload="metadata" src={play.mediaURL(block.media)} />
    <AltInput block={block} set={set} placeholder={t.t("Describe the film")} t={t} />
    <p className="field__hint">{t.t("Phones download a film only when it is played, never ahead.")}</p>
  </>
);

/** Kinds whose block is made from an uploaded file, and what file each takes. */
export const ACCEPT: Partial<Record<BlockKind, string>> = {
  image: "image/*",
  audio: "audio/*",
  video: "video/*",
  hotspots: "image/*",
  overlay: "image/*",
  torch: "image/*",
  call: "audio/*",
  spotdiff: "image/*",
};
