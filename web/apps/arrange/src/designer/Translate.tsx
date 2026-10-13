import { useMemo, useState } from "react";
import type { AdventureMeta, Graph, Translatable } from "@zonventure/graph";
import { adventureTexts, allTexts, coverage, isOutdated, nodeBySlug, nodeTexts, outline, setTranslation } from "@zonventure/graph";
import { Languages } from "lucide-react";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { useT } from "../i18n.tsx";
import { ICON } from "./dz/ui.tsx";
import { STEP } from "./dz/types.tsx";

/**
 * Translating an adventure. The structure — steps, routes, answers — is
 * edited once, in the main language; other languages only add text.
 * Languages in the settings lists every text of the adventure, each with
 * the original beside a box for the translation; the toolbar's switch
 * shows the steps in a translation. Anything left empty plays in the
 * main language.
 *
 * The main language is the adventure's own setting, not the graph's: the
 * designer passes it in as `main`, and the server writes it into the
 * graph as it is saved.
 */

/** Each language in its own words. */
export const LANGUAGE_NAMES: Record<string, string> = {
  sv: "Svenska",
  en: "English",
  de: "Deutsch",
  fi: "Suomi",
  no: "Norsk",
  da: "Dansk",
  fr: "Français",
  es: "Español",
};
export const languageName = (l: string) => LANGUAGE_NAMES[l] ?? l.toUpperCase();

export const translationLanguages = (g: Graph, main: string) => Object.keys(g.translations ?? {}).filter((l) => l !== main).sort();

/** The toolbar's "showing in" switch, with how far each translation has come. */
export function LanguageSwitch({ graph, main, meta, value, onChange }: { graph: Graph; main: string; meta: AdventureMeta; value: string; onChange: (l: string) => void }) {
  const t = useT();
  const others = translationLanguages(graph, main);
  if (!others.length) return null;
  return (
    <label className="lang-switch" title={t.t("The language the steps are shown in")}>
      <Languages size={ICON.sm} aria-hidden="true" />
      <select className="field__input" value={value} onChange={(e) => onChange(e.target.value)} aria-label={t.t("Showing in")}>
        <option value={main}>{languageName(main)}</option>
        {others.map((l) => (
          <option key={l} value={l}>
            {languageName(l)} — {Math.round(coverage(graph, l, meta) * 100)} %
          </option>
        ))}
      </select>
    </label>
  );
}

function TextRows({ texts, graph, lang, onGraphChange, readOnly }: { texts: Translatable[]; graph: Graph; lang: string; onGraphChange: (g: Graph) => void; readOnly?: boolean }) {
  const t = useT();
  const tr = graph.translations?.[lang] ?? {};
  if (!texts.length) return <p className="field__hint">{t.t("Nothing to translate here.")}</p>;
  return (
    <div className="tr-rows">
      {texts.map((x) => (
        <label key={x.key} className="field tr-row">
          <span className="field__label">{t.t(x.label, x.vars)}</span>
          <span className="tr-row__main">{x.main}</span>
          {x.multiline ? (
            <textarea
              className="field__input"
              rows={Math.min(8, Math.max(2, x.main.split("\n").length + 1))}
              value={tr[x.key] ?? ""}
              placeholder={x.answers ? t.t("More accepted answers, one per line") : t.t("Leave empty to show the original")}
              disabled={readOnly}
              onChange={(e) => onGraphChange(setTranslation(graph, lang, x.key, e.target.value, x.main))}
            />
          ) : (
            <input
              className="field__input"
              value={tr[x.key] ?? ""}
              placeholder={t.t("Leave empty to show the original")}
              disabled={readOnly}
              onChange={(e) => onGraphChange(setTranslation(graph, lang, x.key, e.target.value, x.main))}
            />
          )}
          {isOutdated(graph, lang, x) && (
            <span className="field__hint tr-row__outdated">
              {t.t("Outdated: the original has changed since this was translated.")}{" "}
              {!readOnly && (
                <button type="button" className="link-btn" onClick={() => onGraphChange(setTranslation(graph, lang, x.key, tr[x.key] ?? "", x.main))}>
                  {t.t("Still right")}
                </button>
              )}
            </span>
          )}
          {x.answers && <span className="field__hint">{t.t("A team answering in this language may give these too; the original answers still count.")}</span>}
          {x.optional && <span className="field__hint">{t.t("Optional: not counted in the % translated.")}</span>}
        </label>
      ))}
    </div>
  );
}

/**
 * Adventure settings: the main language, the translations, and every
 * text to translate — the adventure's own, then each step's in the order
 * a team meets them. Changing the main language saves the adventure's
 * setting straight away, through onMainChange.
 */
export function LanguagesSettings({ graph, main, meta, onChange, onMainChange, editing, readOnly }: {
  graph: Graph;
  main: string;
  meta: AdventureMeta;
  onChange: (g: Graph) => void;
  onMainChange: (l: string) => Promise<void>;
  /** The language the toolbar shows, translated first. */
  editing: string;
  readOnly?: boolean;
}) {
  const t = useT();
  const [error, setError] = useState<string | null>(null);
  const [pick, setPick] = useState(editing);
  const [onlyMissing, setOnlyMissing] = useState(true);
  const others = translationLanguages(graph, main);
  const addable = Object.keys(LANGUAGE_NAMES).filter((l) => l !== main && !others.includes(l));
  const lang = others.includes(pick) ? pick : others[0];
  // What is left to translate, taken when the language or the filter
  // changes rather than on every edit, so a row stays while it is typed in.
  const missing = useMemo(() => {
    const tr = (lang && graph.translations?.[lang]) || {};
    return new Set(allTexts(graph, meta).filter((x) => !x.answers && !x.optional && (!tr[x.key]?.trim() || isOutdated(graph, lang!, x))).map((x) => x.key));
  }, [lang, onlyMissing]);
  const shown = (texts: Translatable[]) => (onlyMissing ? texts.filter((x) => missing.has(x.key)) : texts);
  const steps = outline(graph).flatMap((r) => (r.kind === "step" ? [nodeBySlug(graph, r.slug)!] : []));
  const rows = readOnly ? { readOnly } : {};
  return (
    <section className="insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.languages} />
        {t.t("Languages")}
      </h3>
      <p className="field__hint">
        {t.t("The adventure is written in its main language. A translation adds the same texts in another language; teams choose which to play in, and anything untranslated shows the original.")}
      </p>
      <label className="field">
        <span className="field__label">{t.t("Main language")}</span>
        <select className="field__input" value={main} onChange={(e) => {
          setError(null);
          onMainChange(e.target.value).catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
        }}>
          {Object.keys(LANGUAGE_NAMES).filter((l) => !others.includes(l)).map((l) => (
            <option key={l} value={l}>{languageName(l)}</option>
          ))}
        </select>
        {error && <span className="field__error" role="alert">{error}</span>}
      </label>
      {others.map((l) => (
        <div key={l} className="box__row">
          <span className="tr-lang">
            <strong>{languageName(l)}</strong> · {t.t("{{pct}} % translated", { pct: Math.round(coverage(graph, l, meta) * 100) })}
          </span>
          <button type="button" className="link-btn" onClick={() => setPick(l)}>{t.t("Translate")}</button>
          <button
            type="button"
            className="link-btn"
            onClick={() => {
              if (!window.confirm(t.t("Remove the {{lang}} translation? Its texts are deleted.", { lang: languageName(l) }))) return;
              const next = { ...(graph.translations ?? {}) };
              delete next[l];
              const sources = { ...(graph.translation_sources ?? {}) };
              delete sources[l];
              onChange({ ...graph, translations: next, translation_sources: sources });
            }}
          >
            {t.t("Remove")}
          </button>
        </div>
      ))}
      {addable.length > 0 && (
        <select className="field__input" value="" aria-label={t.t("Add a translation")}
          onChange={(e) => {
            if (!e.target.value) return;
            onChange({ ...graph, translations: { ...(graph.translations ?? {}), [e.target.value]: {} } });
            setPick(e.target.value);
          }}>
          <option value="">{t.t("+ Add a translation…")}</option>
          {addable.map((l) => (
            <option key={l} value={l}>{languageName(l)}</option>
          ))}
        </select>
      )}
      {lang && (
        <div className="tr-all">
          <div className="tr-all__head">
            <h4>{t.t("Texts in {{lang}}", { lang: languageName(lang) })}</h4>
            <label className="tr-all__filter">
              <input type="checkbox" checked={onlyMissing} onChange={(e) => setOnlyMissing(e.target.checked)} />
              {t.t("Only untranslated")}
            </label>
          </div>
          {onlyMissing && !missing.size && <p className="field__hint">{t.t("Everything is translated.")}</p>}
          {shown(adventureTexts(graph, meta)).length > 0 && (
            <section className="tr-group">
              <h5>{t.t("Title, journal, places and messages")}</h5>
              <TextRows texts={shown(adventureTexts(graph, meta))} graph={graph} lang={lang} onGraphChange={onChange} {...rows} />
            </section>
          )}
          {steps.map((n) => {
            const texts = shown(nodeTexts(n));
            if (!texts.length) return null;
            return (
              <section key={n.slug} className="tr-group">
                <h5>{n.title || t.t(STEP[n.type].label)}</h5>
                <TextRows texts={texts} graph={graph} lang={lang} onGraphChange={onChange} {...rows} />
              </section>
            );
          })}
        </div>
      )}
    </section>
  );
}
