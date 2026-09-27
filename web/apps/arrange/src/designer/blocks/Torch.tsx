import { play } from "@zonventure/api";
import type { TorchStyle } from "@zonventure/graph";
import { AltInput } from "./Media.tsx";
import { withField, type Editor } from "./types.ts";
import "./pictures.css";

const STYLES: { value: TorchStyle | ""; label: string; hint: string }[] = [
  { value: "", label: "Torch", hint: "Teams see the picture in the dark and shine a torch over it with a finger." },
  { value: "scratch", label: "Scratch card", hint: "Teams rub the grey coating off with a finger, and what they uncover stays uncovered." },
  { value: "uv", label: "UV lens", hint: "Teams drag a violet lens over the picture to see hidden ink. Paint the secret faintly into the picture." },
  { value: "red", label: "Red lens", hint: "Teams switch a red lens on to read what red noise hides. Hide the secret in blue or dark lines under red scribbles." },
];

export const TorchEditor: Editor<"torch"> = ({ block, set, t }) => {
  const src = play.mediaURL(block.media);
  const style = STYLES.find((s) => s.value === (block.style ?? "")) ?? STYLES[0]!;
  return (
    <>
      <select className="field__input" value={block.style ?? ""} aria-label={t.t("Style")} onChange={(e) => set(withField(block, "style", (e.target.value || undefined) as TorchStyle | undefined))}>
        {STYLES.map((s) => (
          <option key={s.value} value={s.value}>
            {t.t(s.label)}
          </option>
        ))}
      </select>
      <div className="pic-ed__stage pic-ed__torch" aria-label={t.t("Preview")} role="img">
        <img className="pic-ed__img pic-ed__dark" src={src} alt="" />
        <img className="pic-ed__img pic-ed__lit" src={src} alt="" />
      </div>
      <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
      <p className="field__hint">{t.t(style.hint)}</p>
    </>
  );
};
