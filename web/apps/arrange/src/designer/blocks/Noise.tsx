import { withField, type Editor } from "./types.ts";
import { MarkdownArea } from "../MarkdownArea.tsx";

const DEFAULT = 70;

export const NoiseEditor: Editor<"noise"> = ({ block, set, t }) => {
  const threshold = block.threshold ?? DEFAULT;
  return (
    <>
      <label className="field blk__zoom">
        <span className="field__label">{t.t("How loud: {{n}}", { n: threshold })}</span>
        <input type="range" min={1} max={100} value={threshold} onChange={(e) => set(withField(block, "threshold", Number(e.target.value) === DEFAULT ? undefined : Number(e.target.value)))} />
        <span className="blk__zoom-ends" aria-hidden="true">
          <span>{t.t("Quiet talk")}</span>
          <span>{t.t("Cheering")}</span>
          <span>{t.t("Shouting")}</span>
        </span>
      </label>
      <MarkdownArea
        className="field__input"
        rows={3}
        value={block.text ?? ""}
        placeholder={t.t("Markdown. What the team reads once it is loud enough.")}
        aria-label={t.t("Text when loud enough")}
        onChange={(e) => set(withField(block, "text", e.target.value))}
      />
      <p className="field__hint">{t.t("The phone listens only after the team taps Start, and only measures how loud it is: nothing is recorded or sent.")}</p>
    </>
  );
};

const SHAKE = 60;

export const ShakeEditor: Editor<"shake"> = ({ block, set, t }) => {
  const threshold = block.threshold ?? SHAKE;
  return (
    <>
      <label className="field blk__zoom">
        <span className="field__label">{t.t("How hard: {{n}}", { n: threshold })}</span>
        <input type="range" min={1} max={100} value={threshold} onChange={(e) => set(withField(block, "threshold", Number(e.target.value) === SHAKE ? undefined : Number(e.target.value)))} />
        <span className="blk__zoom-ends" aria-hidden="true">
          <span>{t.t("A jiggle")}</span>
          <span>{t.t("A shake")}</span>
          <span>{t.t("Like a cocktail")}</span>
        </span>
      </label>
      <MarkdownArea
        className="field__input"
        rows={3}
        value={block.text ?? ""}
        placeholder={t.t("Markdown. What the team reads once it has shaken the phone.")}
        aria-label={t.t("Text when shaken")}
        onChange={(e) => set(withField(block, "text", e.target.value))}
      />
      <p className="field__hint">{t.t("The phone reads its motion only after the team taps Start. Hold on tight.")}</p>
    </>
  );
};
