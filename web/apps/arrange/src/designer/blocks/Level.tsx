import { withField, type Editor } from "./types.ts";

export const LevelEditor: Editor<"level"> = ({ block, set, t }) => {
  const up = (block.pitch ?? 0) > 0;
  return (
    <>
      <div className="seg" role="group" aria-label={t.t("How to hold the phone")}>
        <button type="button" className={up ? "" : "is-on"} aria-pressed={!up} onClick={() => set(withField(block, "pitch", undefined))}>
          {t.t("Hold it flat")}
        </button>
        <button type="button" className={up ? "is-on" : ""} aria-pressed={up} onClick={() => up || set({ ...block, pitch: 45 })}>
          {t.t("Point it upwards")}
        </button>
      </div>
      {up && (
        <label className="field field--num">
          <span className="field__label">{t.t("Degrees above the horizon")}</span>
          <input
            className="field__input"
            type="number"
            min={1}
            max={90}
            value={block.pitch}
            onChange={(e) => set({ ...block, pitch: Math.min(90, Math.max(1, Math.round(Number(e.target.value)) || 1)) })}
          />
        </label>
      )}
      <textarea
        className="field__input"
        rows={3}
        value={block.text ?? ""}
        placeholder={t.t("Markdown. What the team reads once the phone is held right.")}
        aria-label={t.t("Text when done")}
        onChange={(e) => set(withField(block, "text", e.target.value))}
      />
      <p className="field__hint">
        {up
          ? t.t("The team aims the back camera that far up, at the top of a tower say, and holds it within 3° for a moment.")
          : t.t("The team holds the phone flat, like a spirit level, within 3° for a moment.")}
      </p>
    </>
  );
};
