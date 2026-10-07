import type { play } from "@zonventure/api";
import type { Theme } from "@zonventure/graph";
import { FONTS, STARTER_GROUPS, STARTER_THEMES, isHex, lookContrast, type ContrastIssue } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { X } from "lucide-react";
import { BackdropPicker, Check, ColourList, Slider } from "./BackdropPicker.tsx";
import { CoverPicker } from "./CoverPicker.tsx";
import { demoAnswer } from "./blocks/Animation.tsx";
import { AdventureSlug } from "./MediaEditor.tsx";
import { adventures, play as playApi } from "@zonventure/api";
import { useContext, useEffect, useRef, useState, type ReactNode } from "react";
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

const ANSWER_FIELDS: { key: "answer_button_color" | "answer_button_text_color"; label: string; hint: string }[] = [
  { key: "answer_button_color", label: "Answer button", hint: "The button in the answer bar." },
  { key: "answer_button_text_color", label: "Answer button text", hint: "Defaults to black or white, whichever reads best." },
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
  /** Keep a starter or organisation theme picked as the main look among the adventure's themes. */
  onAddTheme?: (name: string, look: play.Brand) => void;
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
 * The adventure's main look: one of its saved themes, the organisation's,
 * or a starter, saved at once. One not yet among the adventure's themes is
 * added to them as a copy, to change in the theme editor. A main look that
 * matches no theme (one set before themes, or through the API) shows as
 * custom, and can be saved as a theme to edit.
 */
export function Look({ brand, saving, error, savedThemes, onApply, onAddTheme, onSaveAsTheme, onEditThemes }: Props) {
  const t = useT();
  const [library, setLibrary] = useState<adventures.OrgTheme[]>([]);
  useEffect(() => {
    const ac = new AbortController();
    // Without the library the starters and the adventure's own remain.
    adventures.listOrgThemes(ac.signal).then(setLibrary, () => {});
    return () => ac.abort();
  }, []);
  const standard = Object.keys(brand).length === 0;
  const matched = savedThemes.find((th) => canon(th.look) === canon(brand));
  const value = standard ? "standard" : matched ? `saved:${matched.slug}` : "custom";
  const choose = (v: string) => {
    if (v === "standard") return onApply({});
    const [kind, id] = [v.slice(0, v.indexOf(":")), v.slice(v.indexOf(":") + 1)];
    const saved = kind === "saved" ? savedThemes.find((x) => x.slug === id) : undefined;
    if (saved) return onApply(structuredClone(saved.look));
    const own = kind === "org" ? library.find((x) => String(x.id) === id) : undefined;
    const starter = kind === "starter" ? STARTER_THEMES.find((x) => x.id === id) : undefined;
    const picked = own ? { name: own.name, look: own.look } : starter ? { name: t.t(starter.label), look: starter.look as play.Brand } : null;
    if (!picked) return;
    onApply(structuredClone(picked.look));
    if (!savedThemes.some((th) => canon(th.look) === canon(picked.look))) onAddTheme?.(picked.name, picked.look);
  };
  return (
    <section className="look insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.look} />
        {t.t("Look")}
      </h3>
      <label className="field">
        <span className="field__label">{t.t("Main look")}</span>
        <select className="field__input" value={value} disabled={saving} onChange={(e) => choose(e.target.value)}>
          <option value="standard">{t.t("Standard look")}</option>
          {value === "custom" && <option value="custom" disabled>{t.t("Custom look (not a saved theme)")}</option>}
          {savedThemes.length > 0 && (
            <optgroup label={t.t("This adventure")}>
              {savedThemes.map((th) => (
                <option key={th.slug} value={`saved:${th.slug}`}>{th.name || t.t("Untitled theme")}</option>
              ))}
            </optgroup>
          )}
          {library.length > 0 && (
            <optgroup label={t.t("Your organisation")}>
              {library.map((th) => (
                <option key={th.id} value={`org:${th.id}`}>{th.name}</option>
              ))}
            </optgroup>
          )}
          {STARTER_GROUPS.map((g) => (
            <optgroup key={g.id} label={g.label ? t.t(g.label) : t.t("More looks")}>
              {STARTER_THEMES.filter((s) => s.group === g.id).map((s) => (
                <option key={s.id} value={`starter:${s.id}`}>{t.t(s.label)}</option>
              ))}
            </optgroup>
          ))}
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
 * The look's controls for any look — the adventure's own, a saved theme,
 * an organisation's theme — in folded groups, colours open. Pictures and
 * sounds are an adventure's own media: without media they are not offered.
 */
export function LookControls({ brand, onChange, media = true }: { brand: play.Brand; onChange: (b: play.Brand) => void; media?: boolean }) {
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
  const setStyle = (patch: Partial<Record<keyof play.LookStyle, number | boolean | string | string[] | undefined>>) => {
    const style: Record<string, unknown> = { ...brand.style, ...patch };
    for (const k of Object.keys(style)) if (style[k] === undefined) delete style[k];
    const next = { ...brand };
    if (Object.keys(style).length) next.style = style as play.LookStyle;
    else delete next.style;
    onChange(next);
  };

  return (
    <div className="look__groups">
      <Group title={t.t("Colours")} hint={t.t("Accent, titles, page, text and the answer bar.")} open>
        {FIELDS.map((f) => (
          <ColourRow key={f.key} label={f.label} hint={f.hint} value={brand[f.key]} onSet={(v) => set(f.key, v)} />
        ))}
        <ContrastNote issues={lookContrast(brand)} />
      </Group>
      <Group title={t.t("Text")} hint={t.t("Typefaces, size and how titles look.")}>
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
      </Group>
      <Group title={t.t("Cards and buttons")} hint={t.t("Corners, cards, buttons and the answer bar.")}>
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
        <Check
          label={t.t("Outline cards")}
          checked={!!brand.card_outline}
          onCheck={(v) => {
            const next = { ...brand };
            if (v) next.card_outline = true;
            else delete next.card_outline;
            onChange(next);
          }}
        />
        <Slider
          label={t.t("Card opacity")}
          value={brand.style?.card_opacity ?? 1}
          min={0.2}
          max={1}
          step={0.05}
          shown={(brand.style?.card_opacity ?? 1) >= 1 ? t.t("Solid") : `${Math.round((brand.style?.card_opacity ?? 1) * 100)} %`}
          onSlide={(v) => setStyle({ card_opacity: v >= 1 ? undefined : v })}
        />
        {(brand.style?.card_opacity ?? 1) < 1 && (
          <Check label={t.t("Frost what shows through")} hint={t.t("Keeps text readable over a busy background.")} checked={!!brand.style?.card_blur} onCheck={(v) => setStyle({ card_blur: v || undefined })} />
        )}
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
        {ANSWER_FIELDS.map((f) => (
          <ColourRow
            key={f.key}
            label={f.label}
            hint={f.hint}
            value={brand.style?.[f.key]}
            onSet={(v) => setStyle({ [f.key]: v && isHex(v) ? v.toLowerCase() : undefined })}
          />
        ))}
      </Group>
      <Group title={t.t("Background")} hint={media ? t.t("A pattern or a picture behind the steps, and a cover picture.") : t.t("A pattern behind the steps.")}>
        <BackdropPicker brand={brand} onChange={onChange} media={media} />
        {media && <CoverPicker brand={brand} setStyle={setStyle} />}
      </Group>
      <Group title={t.t("Motion and celebration")} hint={t.t("How steps come in and how answers are met.")}>
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
        <Celebration brand={brand} setStyle={setStyle} media={media} />
      </Group>
    </div>
  );
}

/** A part of the look, folded until wanted: its name and what is in it. */
function Group({ title, hint, open, children }: { title: string; hint: string; open?: boolean; children: ReactNode }) {
  return (
    <details className="look__group" open={open}>
      <summary className="look__group-head">
        <span className="look__group-title">{title}</span>
        <span className="field__hint">{hint}</span>
      </summary>
      <div className="look__group-body">{children}</div>
    </details>
  );
}

/**
 * How a right answer is celebrated, and how both answers sound: confetti
 * or not, its colours or emoji, the words, and sounds of the adventure's
 * own. Try it acts an answer out in the preview.
 */
/** A colour: a swatch, its name and hint, its hex, and a way back to the default. */
function ColourRow({ label, hint, value, onSet }: { label: string; hint: string; value: string | undefined; onSet: (v: string | null) => void }) {
  const t = useT();
  return (
    <div className="look__row">
      <input
        type="color"
        className="look__swatch"
        aria-label={t.t(label)}
        value={value ?? "#ffffff"}
        onChange={(e) => onSet(e.target.value)}
      />
      <div className="look__text">
        <span className="look__label">{t.t(label)}</span>
        <span className="field__hint">{t.t(hint)}</span>
      </div>
      <input
        className="field__input look__hex"
        aria-label={t.t("{{name}} as hex", { name: t.t(label) })}
        placeholder={t.t("default")}
        defaultValue={value ?? ""}
        key={value ?? "none"}
        onBlur={(e) => {
          const raw = e.target.value.trim();
          onSet(raw === "" ? null : raw.startsWith("#") ? raw : "#" + raw);
        }}
      />
      {value && (
        <button type="button" className="icon-remove" onClick={() => onSet(null)} aria-label={t.t("Use the default")}>
          <X size={ICON.sm} />
        </button>
      )}
    </div>
  );
}

function Celebration({ brand, setStyle, media }: { brand: play.Brand; setStyle: (patch: Partial<Record<keyof play.LookStyle, number | boolean | string | string[] | undefined>>) => void; media: boolean }) {
  const t = useT();
  const st = brand.style ?? {};
  return (
    <>
      <Choice
        label={t.t("A right answer")}
        options={[{ value: "", label: t.t("Confetti") }, { value: "quiet", label: t.t("Just the tick") }, { value: "none", label: t.t("Only the words") }]}
        value={st.celebrate ?? ""}
        onPick={(v) => setStyle({ celebrate: v || undefined })}
      />
      {!st.celebrate && (
        <>
          <ColourList
            label={t.t("Confetti colours")}
            empty={t.t("None: the player's own bright mix.")}
            colours={st.confetti_colors ?? []}
            onColours={(c) => setStyle({ confetti_colors: c.length ? c : undefined })}
          />
          <label className="field">
            <span className="field__label">{t.t("Or emoji instead")}</span>
            <input className="field__input" value={st.confetti_emoji ?? ""} maxLength={32} placeholder="🐘 🎉 ⭐" onChange={(e) => setStyle({ confetti_emoji: e.target.value.replace(/\s+/g, "") || undefined })} />
          </label>
        </>
      )}
      <div className="look__pair">
        <label className="field">
          <span className="field__label">{t.t("Right answer says")}</span>
          <input className="field__input" value={st.cheer_text ?? ""} maxLength={40} placeholder={t.t("Correct!")} onChange={(e) => setStyle({ cheer_text: e.target.value || undefined })} />
        </label>
        <label className="field">
          <span className="field__label">{t.t("Wrong answer says")}</span>
          <input className="field__input" value={st.miss_text ?? ""} maxLength={40} placeholder={t.t("Not quite")} onChange={(e) => setStyle({ miss_text: e.target.value || undefined })} />
        </label>
      </div>
      {media && (
        <>
          <SoundPick label={t.t("Sound on a right answer")} id={st.sound_right} onPick={(id) => setStyle({ sound_right: id })} />
          <SoundPick label={t.t("Sound on a wrong answer")} id={st.sound_wrong} onPick={(id) => setStyle({ sound_wrong: id })} />
        </>
      )}
      <div className="look__actions">
        <button type="button" className="btn btn--ghost btn--sm" onClick={() => demoAnswer(true)}>{t.t("Try a right answer")}</button>
        <button type="button" className="btn btn--ghost btn--sm" onClick={() => demoAnswer(false)}>{t.t("Try a wrong answer")}</button>
      </div>
      <p className="field__hint">{t.t("The words are the same in every language the adventure is played in.")}</p>
    </>
  );
}

/** A sound of the adventure's own: upload one, listen to it, take it away. */
function SoundPick({ label, id, onPick }: { label: string; id: number | undefined; onPick: (id: number | undefined) => void }) {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="field">
      <span className="field__label">{label}</span>
      <div className="look__actions">
        {id ? (
          <>
            <button type="button" className="btn btn--ghost btn--sm" onClick={() => void new Audio(playApi.mediaURL(id)).play().catch(() => {})}>▶ {t.t("Listen")}</button>
            <button type="button" className="btn btn--ghost btn--sm" disabled={busy || !slug} onClick={() => input.current?.click()}>{t.t("Replace")}</button>
            <button type="button" className="icon-remove" aria-label={t.t("No sound")} onClick={() => onPick(undefined)}>
              <X size={ICON.sm} />
            </button>
          </>
        ) : (
          <button type="button" className="btn btn--ghost btn--sm" disabled={busy || !slug} onClick={() => input.current?.click()}>
            {busy ? t.t("Uploading…") : t.t("Choose a sound")}
          </button>
        )}
      </div>
      <input
        ref={input}
        type="file"
        accept="audio/*"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (!f) return;
          setBusy(true);
          setError(null);
          adventures
            .uploadMedia(slug, f)
            .then((r) => (r.kind === "audio" ? onPick(r.id) : setError(t.t("That file is not a sound."))))
            .catch((err: unknown) => setError(err instanceof Error ? err.message : t.t("Could not upload the sound.")))
            .finally(() => setBusy(false));
        }}
      />
      {error && <span className="field__hint zone-here__warn">{error}</span>}
    </div>
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
