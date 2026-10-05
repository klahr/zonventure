import type { play } from "@zonventure/api";
import type { Theme } from "@zonventure/graph";
import { FONTS, isHex, lookContrast, type ContrastIssue } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { X } from "lucide-react";
import { BackdropPicker, Check, Slider } from "./BackdropPicker.tsx";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { ICON } from "./dz/ui.tsx";

/** What each typeface is for, in the author's words. */
const FONT_LABELS: Record<string, string> = {
  inconsolata: "Terminal",
  "special-elite": "Typewriter",
  merriweather: "Book",
  fredoka: "Playful",
  "baloo-2": "Bubbly",
  "patrick-hand": "Handwritten",
  chewy: "Cartoon",
  "luckiest-guy": "Comic",
  nunito: "Friendly",
  inter: "Clean",
  creepster: "Spooky",
  pacifico: "Romantic",
  vt323: "Retro terminal",
  orbitron: "Futuristic",
  "press-start-2p": "Arcade",
};

/** Display faces, legible only large: offered for titles, not the text. */
const TITLE_ONLY = new Set(["chewy", "luckiest-guy", "creepster", "pacifico", "vt323", "orbitron", "press-start-2p"]);

type ColourKey = "accent" | "accent_secondary" | "background" | "text" | "submit_bg";

const FIELDS: { key: ColourKey; label: string; hint: string }[] = [
  { key: "accent", label: "Accent", hint: "Buttons, the map pin, the score." },
  { key: "accent_secondary", label: "Titles", hint: "Step titles and headings. Defaults to the accent." },
  { key: "background", label: "Page", hint: "Behind everything." },
  { key: "text", label: "Text", hint: "Prompts and story text." },
  { key: "submit_bg", label: "Answer bar", hint: "The bar at the bottom with the answer box." },
];

/** The presets beside the colours: one pick each, never a raw value. */
const CORNERS: { value: NonNullable<play.Brand["corners"]>; label: string }[] = [
  { value: "rounded", label: "Rounded" },
  { value: "soft", label: "Soft" },
  { value: "sharp", label: "Sharp" },
];
const CARDS: { value: NonNullable<play.Brand["cards"]>; label: string }[] = [
  { value: "raised", label: "Raised" },
  { value: "flat", label: "Flat" },
];

interface Props {
  brand: play.Brand;
  saving: boolean;
  error: string | null;
  savedThemes: Theme[];
  /** Make this the adventure's main look, saved at once; {} is the standard look. */
  onApply: (b: play.Brand) => void;
  /** Save the main look as a new theme, to edit it in the theme editor. */
  onSaveAsTheme?: () => void;
  /** Open the theme editor. */
  onEditThemes?: () => void;
}

/** A look as JSON with its keys in order, so two can be compared however they were built. */
function canon(v: unknown): string {
  return JSON.stringify(v, (_, x: unknown) =>
    x && typeof x === "object" && !Array.isArray(x) ? Object.fromEntries(Object.entries(x as Record<string, unknown>).sort(([p], [q]) => (p < q ? -1 : 1))) : x,
  );
}

/**
 * The adventure's main look, picked from its saved themes. Looks are
 * made and changed in the theme editor; here one is chosen, and saved at
 * once. A main look that matches no theme (one set before themes, or
 * through the API) shows as custom, and can be saved as a theme to edit.
 */
export function Look({ brand, saving, error, savedThemes, onApply, onSaveAsTheme, onEditThemes }: Props) {
  const t = useT();
  const standard = Object.keys(brand).length === 0;
  const matched = savedThemes.find((th) => canon(th.look) === canon(brand));
  const value = standard ? "standard" : (matched?.slug ?? "custom");
  return (
    <section className="look insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.look} />
        {t.t("Look")}
      </h3>
      <label className="field">
        <span className="field__label">{t.t("Main look")}</span>
        <select
          className="field__input"
          value={value}
          disabled={saving}
          onChange={(e) => {
            if (e.target.value === "standard") return onApply({});
            const th = savedThemes.find((x) => x.slug === e.target.value);
            if (th) onApply(structuredClone(th.look));
          }}
        >
          <option value="standard">{t.t("Standard look")}</option>
          {savedThemes.map((th) => (
            <option key={th.slug} value={th.slug}>{th.name || t.t("Untitled theme")}</option>
          ))}
          {value === "custom" && <option value="custom" disabled>{t.t("Custom look (not a saved theme)")}</option>}
        </select>
      </label>
      {error && <div className="alert alert--danger">{error}</div>}
      <div className="look__actions">
        {onEditThemes && <button type="button" className="btn btn--ghost btn--sm" onClick={onEditThemes}>{t.t("Edit themes…")}</button>}
        {value === "custom" && onSaveAsTheme && <button type="button" className="btn btn--ghost btn--sm" onClick={onSaveAsTheme}>{t.t("Save this look as a theme")}</button>}
      </div>
      <p className="field__hint">
        {t.t("Choosing a look applies it at once, also to running events. Changing a theme later does not change the main look: choose it again to apply the changes.")}
      </p>
    </section>
  );
}

/**
 * The look's controls — colours, typefaces, corners, cards and the
 * background pattern — for any look: the adventure's own here, a saved
 * theme in the theme editor.
 */
export function LookControls({ brand, onChange }: { brand: play.Brand; onChange: (b: play.Brand) => void }) {
  const t = useT();
  const set = (key: ColourKey, v: string | null) => {
    const next = { ...brand };
    if (v && isHex(v)) next[key] = v.toLowerCase();
    else delete next[key];
    onChange(next);
  };

  // A preset key, or its default ("" in storage) when it is the first choice.
  const pick = <K extends "font" | "heading_font" | "corners" | "cards">(key: K, v: string, fallback = "") => {
    const next = { ...brand };
    if (v && v !== fallback) (next as Record<string, string>)[key] = v;
    else delete next[key];
    onChange(next);
  };

  // The finer choices: a field set to undefined goes back to its default.
  const setStyle = (patch: Partial<Record<keyof play.LookStyle, number | boolean | string | undefined>>) => {
    const style: Record<string, unknown> = { ...brand.style, ...patch };
    for (const k of Object.keys(style)) if (style[k] === undefined) delete style[k];
    const next = { ...brand };
    if (Object.keys(style).length) next.style = style as play.LookStyle;
    else delete next.style;
    onChange(next);
  };

  return (
    <>
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
              <button type="button" className="icon-remove" onClick={() => set(f.key, null)} aria-label={t.t("Use the default")}>
                <X size={ICON.sm} />
              </button>
            )}
          </div>
        );
      })}
      <ContrastNote issues={lookContrast(brand)} />
      <h4>{t.t("Style")}</h4>
      <label className="field">
        <span className="field__label">{t.t("Typeface")}</span>
        <select className="field__input" value={brand.font ?? ""} onChange={(e) => pick("font", e.target.value)}>
          <option value="">{t.t("Standard")}</option>
          {Object.entries(FONT_LABELS).filter(([k]) => !TITLE_ONLY.has(k) || brand.font === k).map(([k, label]) => (
            <option key={k} value={k}>{t.t(label)} — {FONTS[k]}</option>
          ))}
        </select>
      </label>
      <label className="field">
        <span className="field__label">{t.t("Titles typeface")}</span>
        <select className="field__input" value={brand.heading_font ?? ""} onChange={(e) => pick("heading_font", e.target.value)}>
          <option value="">{t.t("Same as the text")}</option>
          {Object.entries(FONT_LABELS).map(([k, label]) => (
            <option key={k} value={k}>{t.t(label)} — {FONTS[k]}</option>
          ))}
        </select>
      </label>
      <Choice
        label={t.t("Corners")}
        options={CORNERS.map((c) => ({ value: c.value, label: t.t(c.label) }))}
        value={brand.corners ?? "rounded"}
        onPick={(v) => pick("corners", v, "rounded")}
      />
      <Choice
        label={t.t("Cards")}
        options={CARDS.map((c) => ({ value: c.value, label: t.t(c.label) }))}
        value={brand.cards ?? "raised"}
        onPick={(v) => pick("cards", v, "raised")}
      />
      <label className="look__check">
        <input
          type="checkbox"
          checked={!!brand.card_outline}
          onChange={(e) => {
            const next = { ...brand };
            if (e.target.checked) next.card_outline = true;
            else delete next.card_outline;
            onChange(next);
          }}
        />
        <span>{t.t("Outline cards")}</span>
      </label>
      <Slider
        label={t.t("Card opacity")}
        value={brand.style?.card_opacity ?? 1}
        min={0.2}
        max={1}
        step={0.05}
        shown={(brand.style?.card_opacity ?? 1) >= 1 ? t.t("Solid") : `${Math.round((brand.style?.card_opacity ?? 1) * 100)} %`}
        onSlide={(v) => setStyle({ card_opacity: v >= 1 ? undefined : v })}
      />
      <label className="field">
        <span className="field__label">{t.t("Step transition")}</span>
        <select className="field__input" value={brand.style?.transition ?? ""} onChange={(e) => setStyle({ transition: (e.target.value || undefined) as never })}>
          <option value="">{t.t("Gentle rise (standard)")}</option>
          <option value="fade">{t.t("Fade")}</option>
          <option value="slide">{t.t("Slide in")}</option>
          <option value="zoom">{t.t("Zoom")}</option>
          <option value="flip">{t.t("Flip")}</option>
          <option value="pop">{t.t("Pop")}</option>
          <option value="none">{t.t("No movement")}</option>
        </select>
        <span className="field__hint">{t.t("How each new step comes in. Phones set to reduce motion skip it.")}</span>
      </label>
      {(brand.style?.card_opacity ?? 1) < 1 && (
        <Check label={t.t("Frost what shows through")} hint={t.t("Keeps text readable over a busy background.")} checked={!!brand.style?.card_blur} onCheck={(v) => setStyle({ card_blur: v || undefined })} />
      )}
      <h4>{t.t("Buttons and text")}</h4>
      <Choice
        label={t.t("Button shape")}
        options={[{ value: "", label: t.t("Pill") }, { value: "rounded", label: t.t("Rounded") }, { value: "square", label: t.t("Square") }]}
        value={brand.style?.button_shape ?? ""}
        onPick={(v) => setStyle({ button_shape: v || undefined })}
      />
      <Choice
        label={t.t("Button fill")}
        options={[{ value: "", label: t.t("Classic") }, { value: "gradient", label: t.t("Gradient") }, { value: "flat", label: t.t("Flat") }, { value: "outline", label: t.t("Outline") }, { value: "soft", label: t.t("Soft") }]}
        value={brand.style?.button_fill ?? ""}
        onPick={(v) => setStyle({ button_fill: v || undefined })}
      />
      <Choice
        label={t.t("Button size")}
        options={[{ value: "small", label: t.t("Small") }, { value: "", label: t.t("Normal") }, { value: "large", label: t.t("Large") }]}
        value={brand.style?.button_size ?? ""}
        onPick={(v) => setStyle({ button_size: v || undefined })}
      />
      <Choice
        label={t.t("Answer bar")}
        options={[{ value: "", label: t.t("Along the bottom") }, { value: "floating", label: t.t("Floating") }]}
        value={brand.style?.dock ?? ""}
        onPick={(v) => setStyle({ dock: v || undefined })}
      />
      <Slider
        label={t.t("Text size")}
        value={brand.style?.text_scale ?? 1}
        min={0.85}
        max={1.4}
        step={0.05}
        shown={`${Math.round((brand.style?.text_scale ?? 1) * 100)} %`}
        onSlide={(v) => setStyle({ text_scale: Math.abs(v - 1) < 0.001 ? undefined : v })}
      />
      <Choice
        label={t.t("Titles")}
        options={[{ value: "", label: t.t("Plain") }, { value: "ribbon", label: t.t("Ribbon") }, { value: "underline", label: t.t("Underline") }, { value: "shadow", label: t.t("Shadow") }]}
        value={brand.style?.title_style ?? ""}
        onPick={(v) => setStyle({ title_style: v || undefined })}
      />
      <Check label={t.t("Titles in capitals")} checked={!!brand.style?.title_upper} onCheck={(v) => setStyle({ title_upper: v || undefined })} />
      <BackdropPicker brand={brand} onChange={onChange} />
    </>
  );
}

/** Where the look is hard to read, in the author's words, with the ratio to aim for. */
function ContrastNote({ issues }: { issues: ContrastIssue[] }) {
  const t = useT();
  if (!issues.length) return null;
  const what = { text: t.t("Text on the page"), titles: t.t("Titles on the page"), cards: t.t("Text on see-through cards over the background"), buttons: t.t("Outlined or soft buttons on the page") };
  return (
    <div className="alert alert--warn look__contrast" role="status">
      <strong>{t.t("Hard to read outdoors")}</strong>
      <ul>
        {issues.map((i) => (
          <li key={i.key}>{t.t("{{what}}: {{ratio}}:1, aim for at least {{min}}:1.", { what: what[i.key], ratio: i.ratio, min: i.min })}</li>
        ))}
      </ul>
    </div>
  );
}

/** A preset as a row of segments: every option visible, one tap to pick. */
function Choice(props: { label: string; options: { value: string; label: string }[]; value: string; onPick: (v: string) => void }) {
  return (
    <div className="field" role="radiogroup" aria-label={props.label}>
      <span className="field__label">{props.label}</span>
      <div className="look__segments">
        {props.options.map((o) => (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={props.value === o.value}
            className={"look__segment" + (props.value === o.value ? " is-on" : "")}
            onClick={() => props.onPick(o.value)}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  );
}
