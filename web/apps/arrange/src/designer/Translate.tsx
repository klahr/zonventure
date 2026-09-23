import type { Graph, Node, Translatable } from "@zonventure/graph";
import { adventureTexts, coverage, nodeTexts, setTranslation } from "@zonventure/graph";
import { Languages, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { Button, ICON } from "./dz/ui.tsx";
import { STEP } from "./dz/types.tsx";

/**
 * Translating an adventure. The structure — steps, routes, answers — is
 * edited once, in the main language; other languages only add text.
 * Choosing a language to edit in the toolbar turns the inspector into a
 * list of the step's texts, each with the original beside a box for the
 * translation. Anything left empty plays in the main language.
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

export const mainLanguage = (g: Graph) => g.language || "sv";
export const translationLanguages = (g: Graph) => Object.keys(g.translations ?? {}).filter((l) => l !== mainLanguage(g)).sort();

/** The toolbar's "editing in" switch, with how far each translation has come. */
export function LanguageSwitch({ graph, value, onChange }: { graph: Graph; value: string; onChange: (l: string) => void }) {
  const t = useT();
  const main = mainLanguage(graph);
  const others = translationLanguages(graph);
  if (!others.length) return null;
  return (
    <label className="lang-switch" title={t.t("The language you are editing the text in")}>
      <Languages size={ICON.sm} aria-hidden="true" />
      <select className="field__input" value={value} onChange={(e) => onChange(e.target.value)} aria-label={t.t("Editing in")}>
        <option value={main}>{languageName(main)}</option>
        {others.map((l) => (
          <option key={l} value={l}>
            {languageName(l)} — {Math.round(coverage(graph, l) * 100)} %
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
          <span className="field__label">{t.t(x.label)}</span>
          <span className="tr-row__main">{x.main}</span>
          {x.multiline ? (
            <textarea
              className="field__input"
              rows={Math.min(8, Math.max(2, x.main.split("\n").length + 1))}
              value={tr[x.key] ?? ""}
              placeholder={x.answers ? t.t("More accepted answers, one per line") : t.t("Leave empty to show the original")}
              disabled={readOnly}
              onChange={(e) => onGraphChange(setTranslation(graph, lang, x.key, e.target.value))}
            />
          ) : (
            <input
              className="field__input"
              value={tr[x.key] ?? ""}
              placeholder={t.t("Leave empty to show the original")}
              disabled={readOnly}
              onChange={(e) => onGraphChange(setTranslation(graph, lang, x.key, e.target.value))}
            />
          )}
          {x.answers && <span className="field__hint">{t.t("A team answering in this language may give these too; the original answers still count.")}</span>}
        </label>
      ))}
    </div>
  );
}

/** The inspector while translating: the step's texts, original beside translation. */
export function TranslateStep({ node, graph, lang, onGraphChange, onClose, readOnly }: { node: Node; graph: Graph; lang: string; onGraphChange: (g: Graph) => void; onClose: () => void; readOnly?: boolean }) {
  const t = useT();
  const kind = STEP[node.type];
  const Icon = kind.icon;
  return (
    <>
      <div className="inspector-head">
        <span className="inspector-identity">
          <span className={`inspector-drop-icon dz-step-icon dz-step-icon--${node.type}`} aria-hidden="true">
            <Icon size={ICON.lg} />
          </span>
          <span className="inspector-identity-text">
            <span className="inspector-name">{node.title || t.t(kind.label)}</span>
            <span className="inspector-subtitle">{t.t("Translating into {{lang}}", { lang: languageName(lang) })}</span>
          </span>
        </span>
        <span className="inspector-head-right">
          <Button variant="ghost" size="icon" className="inspector-close" onClick={onClose} aria-label={t.t("Close inspector")} title={t.t("Close inspector")}>
            <X size={ICON.md} />
          </Button>
        </span>
      </div>
      <div className="inspector-body">
        <div className="insp">
          <TextRows texts={nodeTexts(node)} graph={graph} lang={lang} onGraphChange={onGraphChange} {...(readOnly ? { readOnly } : {})} />
        </div>
      </div>
    </>
  );
}

/** Adventure settings: the main language, the translations, and their adventure-wide texts. */
export function LanguagesSettings({ graph, onChange, editing, onEditing }: { graph: Graph; onChange: (g: Graph) => void; editing: string; onEditing: (l: string) => void }) {
  const t = useT();
  const main = mainLanguage(graph);
  const others = translationLanguages(graph);
  const addable = Object.keys(LANGUAGE_NAMES).filter((l) => l !== main && !others.includes(l));
  const lang = others.includes(editing) ? editing : others[0];
  return (
    <section className="insp__section">
      <h3>{t.t("Languages")}</h3>
      <p className="field__hint">
        {t.t("The adventure is written in its main language. A translation adds the same texts in another language; teams choose which to play in, and anything untranslated shows the original.")}
      </p>
      <label className="field">
        <span className="field__label">{t.t("Main language")}</span>
        <select className="field__input" value={main} onChange={(e) => onChange({ ...graph, language: e.target.value })}>
          {Object.keys(LANGUAGE_NAMES).filter((l) => !others.includes(l)).map((l) => (
            <option key={l} value={l}>{languageName(l)}</option>
          ))}
        </select>
      </label>
      {others.map((l) => (
        <div key={l} className="box__row">
          <span className="tr-lang">
            <strong>{languageName(l)}</strong> · {t.t("{{pct}} % translated", { pct: Math.round(coverage(graph, l) * 100) })}
          </span>
          <button type="button" className="link-btn" onClick={() => onEditing(l)}>{t.t("Translate")}</button>
          <button
            type="button"
            className="link-btn"
            onClick={() => {
              if (!window.confirm(t.t("Remove the {{lang}} translation? Its texts are deleted.", { lang: languageName(l) }))) return;
              const next = { ...(graph.translations ?? {}) };
              delete next[l];
              onChange({ ...graph, translations: next });
              if (editing === l) onEditing(main);
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
            onChange({ ...graph, language: main, translations: { ...(graph.translations ?? {}), [e.target.value]: {} } });
            onEditing(e.target.value);
          }}>
          <option value="">{t.t("+ Add a translation…")}</option>
          {addable.map((l) => (
            <option key={l} value={l}>{languageName(l)}</option>
          ))}
        </select>
      )}
      {lang && (
        <details className="box" open={editing === lang}>
          <summary>{t.t("Journal, places and messages in {{lang}}", { lang: languageName(lang) })}</summary>
          <TextRows texts={adventureTexts(graph)} graph={graph} lang={lang} onGraphChange={onChange} />
        </details>
      )}
    </section>
  );
}
