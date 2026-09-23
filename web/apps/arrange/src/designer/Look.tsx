import type { play } from "@zonventure/api";
import { FONTS, isHex } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";

/**
 * The adventure's palette, edited beside the phone preview so every
 * change shows on the step at once. These are the five colours the lane
 * player already understood (internal/adventure: Colors), so an
 * adventure styled for either player looks the same in both.
 *
 * Only #rrggbb is kept — the colour picker produces nothing else, and
 * the text field is checked before a value is accepted. Contrast is not
 * the author's job: text on the accent and on the answer bar is picked
 * automatically (play-ui/brand.ts).
 */
/** What each typeface is for, in the author's words. */
const FONT_LABELS: Record<string, string> = {
  inconsolata: "Terminal",
  "special-elite": "Typewriter",
  merriweather: "Book",
  fredoka: "Playful",
};

const FIELDS: { key: keyof play.Brand; label: string; hint: string }[] = [
  { key: "accent", label: "Accent", hint: "Buttons, the map pin, the score." },
  { key: "accent_secondary", label: "Titles", hint: "Step titles and headings. Defaults to the accent." },
  { key: "background", label: "Page", hint: "Behind everything." },
  { key: "text", label: "Text", hint: "Prompts and story text." },
  { key: "submit_bg", label: "Answer bar", hint: "The bar at the bottom with the answer box." },
];

interface Props {
  brand: play.Brand;
  dirty: boolean;
  saving: boolean;
  error: string | null;
  onChange: (b: play.Brand) => void;
  onSave: () => void;
  onRevert: () => void;
}

export function Look({ brand, dirty, saving, error, onChange, onSave, onRevert }: Props) {
  const t = useT();
  const set = (key: keyof play.Brand, v: string | null) => {
    const next = { ...brand };
    if (v && isHex(v)) next[key] = v.toLowerCase();
    else delete next[key];
    onChange(next);
  };

  return (
    <section className="look insp__section">
      <h3>{t.t("Colours")}</h3>
      <p className="field__hint">{t.t("How this adventure looks to players. The preview above changes as you pick.")}</p>
      {FIELDS.map((f) => {
        const v = brand[f.key];
        return (
          <div key={f.key} className="look__row">
            <input
              type="color"
              className="look__swatch"
              aria-label={t.t(f.label)}
              value={v ?? "#ffffff"}
              onChange={(e) => set(f.key, e.target.value)}
            />
            <div className="look__text">
              <span className="look__label">{t.t(f.label)}</span>
              <span className="field__hint">{t.t(f.hint)}</span>
            </div>
            <input
              className="field__input look__hex"
              aria-label={t.t("{{name}} as hex", { name: t.t(f.label) })}
              placeholder={t.t("default")}
              defaultValue={v ?? ""}
              key={v ?? "none"}
              onBlur={(e) => {
                const raw = e.target.value.trim();
                set(f.key, raw === "" ? null : raw.startsWith("#") ? raw : "#" + raw);
              }}
            />
            {v && (
              <button type="button" className="link-btn" onClick={() => set(f.key, null)} aria-label={t.t("Use the default")}>
                ×
              </button>
            )}
          </div>
        );
      })}
      <label className="field">
        <span className="field__label">{t.t("Typeface")}</span>
        <select className="field__input" value={brand.font ?? ""} onChange={(e) => {
          const next = { ...brand };
          if (e.target.value) next.font = e.target.value;
          else delete next.font;
          onChange(next);
        }}>
          <option value="">{t.t("Standard")}</option>
          {Object.entries(FONT_LABELS).map(([k, label]) => (
            <option key={k} value={k}>{t.t(label)} — {FONTS[k]}</option>
          ))}
        </select>
      </label>
      {error && <div className="alert alert--danger">{error}</div>}
      <div className="look__actions">
        <button type="button" className="btn btn--primary btn--sm" disabled={!dirty || saving} onClick={onSave}>
          {saving ? t.t("Saving…") : t.t("Save colours")}
        </button>
        {dirty && (
          <button type="button" className="btn btn--ghost btn--sm" disabled={saving} onClick={onRevert}>
            {t.t("Undo changes")}
          </button>
        )}
      </div>
      <p className="field__hint">{t.t("Colours belong to the adventure, not to a saved version: they apply to running events too.")}</p>
    </section>
  );
}
