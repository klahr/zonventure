import { useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { play } from "@zonventure/api";
import type { AdventureMeta, Graph, Translatable, TranslatableFile } from "@zonventure/graph";
import { adventureFiles, adventureTexts, allTexts, coverage, isFileOutdated, isOutdated, nodeBySlug, nodeFiles, nodeTexts, outline, setTranslatedFile, setTranslation, translatedFile, translationOriginal, wordDiff } from "@zonventure/graph";
import { BookMarked, Check, CheckCircle2, ChevronRight, ClipboardCopy, Languages, Plus, Search, Trash2, Undo2, Upload, X } from "lucide-react";
import { useMediaUpload } from "./MediaEditor.tsx";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { useT } from "../i18n.tsx";
import { Button, ICON, Modal } from "./dz/ui.tsx";
import { STEP } from "./dz/types.tsx";
import { LANGUAGE_NAMES, languageName } from "../languages.ts";

/**
 * Translating an adventure. The structure — steps, routes, answers — is
 * edited once, in the main language; other languages only add text.
 * The translator lists every text of the adventure, the original beside
 * a box for the translation, in the side-by-side way translation tools
 * do; the adventure settings show how far each language has come; the
 * toolbar's switch shows the steps in a translation. Anything left empty
 * plays in the main language. A picture, sound or film may have a file of
 * its own in a language too, such as a recording in that language; one
 * without plays the original's.
 *
 * The main language is the adventure's own setting, not the graph's: the
 * designer passes it in as `main`, and the server writes it into the
 * graph as it is saved.
 */

export { LANGUAGE_NAMES, languageName };

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

/** Where one text stands in a translation. */
type Status = "done" | "todo" | "outdated" | "extra";

function statusOf(graph: Graph, lang: string, x: Translatable): Status {
  if (x.answers || x.optional) return "extra";
  if (isOutdated(graph, lang, x)) return "outdated";
  return graph.translations?.[lang]?.[x.key]?.trim() ? "done" : "todo";
}

/** How far a translation has come, in texts: the ones counted in its %. */
function progress(graph: Graph, lang: string, meta: AdventureMeta) {
  let done = 0, todo = 0, outdated = 0;
  for (const x of allTexts(graph, meta)) {
    const s = statusOf(graph, lang, x);
    if (s === "done") done++;
    else if (s === "todo") todo++;
    else if (s === "outdated") outdated++;
  }
  return { done, todo, outdated, total: done + todo + outdated };
}

const pct = (graph: Graph, lang: string, meta: AdventureMeta) => Math.round(coverage(graph, lang, meta) * 100);

function LangCode({ lang }: { lang: string }) {
  return <span className="tr-code" aria-hidden="true">{lang.toUpperCase()}</span>;
}

function Meter({ value }: { value: number }) {
  return (
    <span className={"tr-meter" + (value >= 100 ? " is-full" : "")} aria-hidden="true">
      <span style={{ width: `${value}%` }} />
    </span>
  );
}

export function removeTranslation(graph: Graph, l: string): Graph {
  const next = { ...(graph.translations ?? {}) };
  delete next[l];
  const originals = { ...(graph.translation_originals ?? {}) };
  delete originals[l];
  const sources = { ...(graph.translation_sources ?? {}) };
  delete sources[l];
  return { ...graph, translations: next, translation_originals: originals, translation_sources: sources };
}

/** The main language, as a select; a language with a translation can't be it. */
function MainLanguage({ graph, main, onMainChange, readOnly }: { graph: Graph; main: string; onMainChange: (l: string) => Promise<void>; readOnly?: boolean }) {
  const t = useT();
  const id = useId();
  const [error, setError] = useState<string | null>(null);
  const others = translationLanguages(graph, main);
  return (
    <div className="tr-main">
      <label className="tr-main__label" htmlFor={id}>{t.t("Main language")}</label>
      <select
        id={id}
        className="field__input"
        value={main}
        disabled={readOnly}
        onChange={(e) => {
          setError(null);
          onMainChange(e.target.value).catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
        }}
      >
        {Object.keys(LANGUAGE_NAMES).filter((l) => !others.includes(l)).map((l) => (
          <option key={l} value={l}>{languageName(l)}</option>
        ))}
      </select>
      {error && <span className="field__error" role="alert">{error}</span>}
    </div>
  );
}

/**
 * Adventure settings: the main language and how far each translation has
 * come. The texts themselves are translated in the translator, which
 * onTranslate opens — on a language, or to add one.
 */
export function LanguagesSettings({ graph, main, meta, onMainChange, onTranslate, readOnly }: {
  graph: Graph;
  main: string;
  meta: AdventureMeta;
  onMainChange: (l: string) => Promise<void>;
  onTranslate: (lang?: string) => void;
  readOnly?: boolean;
}) {
  const t = useT();
  const others = translationLanguages(graph, main);
  return (
    <section className="insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.languages} />
        {t.t("Languages")}
      </h3>
      <p className="field__hint">
        {t.t("The adventure is written in its main language. A translation adds the same texts in another language; teams choose which to play in, and anything untranslated shows the original.")}
      </p>
      <MainLanguage graph={graph} main={main} onMainChange={onMainChange} {...(readOnly ? { readOnly } : {})} />
      {others.length > 0 && (
        <ul className="tr-summary">
          {others.map((l) => (
            <li key={l}>
              <button type="button" className="tr-summary__row" onClick={() => onTranslate(l)}>
                <LangCode lang={l} />
                <span className="tr-summary__name">{languageName(l)}</span>
                <Meter value={pct(graph, l, meta)} />
                <span className="tr-pct">{pct(graph, l, meta)} %</span>
                <ChevronRight size={ICON.sm} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <Button size="sm" onClick={() => onTranslate()} icon={<Languages size={ICON.sm} />}>
        {others.length ? t.t("Open the translator") : t.t("Translate the adventure")}
      </Button>
    </section>
  );
}

type Filter = "all" | "todo" | "outdated";

/**
 * The translator: the languages down the side, the texts of the chosen
 * one in two columns — the original, and a box for the translation —
 * grouped as a team meets them: the adventure's own texts, then each
 * step. Each text shows where it stands; a filter narrows the list to
 * what is left, a search to what is wanted. Ctrl+Enter goes to the next
 * text, as in a translation tool.
 */
export function TranslatorModal({ graph, main, meta, onChange, onMainChange, onRemove, initial, onClose, readOnly }: {
  graph: Graph;
  main: string;
  meta: AdventureMeta;
  onChange: (g: Graph) => void;
  onMainChange: (l: string) => Promise<void>;
  /** Removes a language, asking first: one with texts takes the password. */
  onRemove: (l: string) => void;
  /** The language to open on: the toolbar's, or one picked in the settings. */
  initial?: string;
  onClose: () => void;
  readOnly?: boolean;
}) {
  const t = useT();
  const others = translationLanguages(graph, main);
  const addable = Object.keys(LANGUAGE_NAMES).filter((l) => l !== main && !others.includes(l));
  const [pick, setPick] = useState(initial ?? "");
  const lang = others.includes(pick) ? pick : others[0];
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const list = useRef<HTMLDivElement>(null);

  const add = (l: string) => {
    onChange({ ...graph, translations: { ...(graph.translations ?? {}), [l]: {} } });
    setPick(l);
    setAdding(false);
  };

  // What the filter lets through, taken when the language or the filter
  // changes rather than on every edit, so a row stays while it is typed in.
  const kept = useMemo(() => {
    if (!lang || filter === "all") return null;
    return new Set(allTexts(graph, meta).filter((x) => statusOf(graph, lang, x) === filter || (filter === "todo" && statusOf(graph, lang, x) === "outdated")).map((x) => x.key));
  }, [lang, filter]);
  const q = query.trim().toLowerCase();
  const tr = (lang && graph.translations?.[lang]) || {};
  const shown = (texts: Translatable[]) =>
    texts.filter((x) => (!kept || kept.has(x.key)) && (!q || x.main.toLowerCase().includes(q) || (tr[x.key] ?? "").toLowerCase().includes(q) || t.t(x.label, x.vars).toLowerCase().includes(q)));
  // A file is never to do — it plays the original — but may be outdated.
  const shownFiles = (files: TranslatableFile[]) =>
    files.filter((f) => (filter === "all" || (filter === "outdated" && lang && isFileOutdated(graph, lang, f))) && (!q || t.t(f.label, f.vars).toLowerCase().includes(q)));

  const groups: { key: string; title: ReactNode; icon: ReactNode; texts: Translatable[]; files: TranslatableFile[] }[] = [
    { key: "adventure", title: t.t("Title, journal, places, items and messages"), icon: <BookMarked size={ICON.sm} />, texts: shown(adventureTexts(graph, meta)), files: shownFiles(adventureFiles(graph)) },
    ...outline(graph).flatMap((r) => {
      if (r.kind !== "step") return [];
      const n = nodeBySlug(graph, r.slug)!;
      const Icon = STEP[n.type].icon;
      return [{
        key: n.slug,
        title: n.title || t.t(STEP[n.type].label),
        icon: <span className={`dz-step-icon dz-step-icon--${n.type}`}><Icon size={ICON.xs} /></span>,
        texts: shown(nodeTexts(n)),
        files: shownFiles(nodeFiles(n)),
      }];
    }),
  ].filter((g) => g.texts.length || g.files.length);

  const p = lang ? progress(graph, lang, meta) : null;

  // Ctrl/Cmd+Enter: on to the next box, as a translation tool does.
  const next = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key !== "Enter" || !(e.ctrlKey || e.metaKey)) return;
    e.preventDefault();
    const boxes = [...(list.current?.querySelectorAll<HTMLElement>(".tr-input") ?? [])];
    boxes[boxes.indexOf(e.currentTarget) + 1]?.focus();
  };

  const filters: { id: Filter; label: string; n?: number }[] = [
    { id: "all", label: t.t("All") },
    { id: "todo", label: t.t("To do"), ...(p ? { n: p.todo + p.outdated } : {}) },
    { id: "outdated", label: t.t("Outdated"), ...(p ? { n: p.outdated } : {}) },
  ];

  return (
    <Modal
      title={t.t("Translations")}
      icon={<Languages size={ICON.md} />}
      subtitle={t.t("Anything left empty plays in {{lang}}, the main language.", { lang: languageName(main) })}
      size="xl"
      flush
      className="tr-modal"
      onClose={onClose}
      footer={
        <>
          {p && (
            <span className="tr-foot__status">
              {t.t("{{done}} of {{total}} texts translated", { done: p.done, total: p.total })}
              {p.outdated > 0 && <> · <span className="tr-foot__warn">{t.t("{{n}} outdated", { n: p.outdated })}</span></>}
            </span>
          )}
          {lang && !readOnly && (
            <span className="tr-foot__keys">
              <kbd>Ctrl</kbd>+<kbd>Enter</kbd> {t.t("next text")}
            </span>
          )}
          <Button variant="primary" onClick={onClose}>{t.t("Done")}</Button>
        </>
      }
    >
      <div className="rail-layout">
        <aside className="rail" aria-label={t.t("Languages")}>
          <div className="rail__group">
            <span className="rail__head">{t.t("Source")}</span>
            <div className="tr-lang tr-lang--source">
              <LangCode lang={main} />
              <span className="tr-lang__name">{languageName(main)}</span>
            </div>
            <MainLanguage graph={graph} main={main} onMainChange={onMainChange} {...(readOnly ? { readOnly } : {})} />
          </div>
          <div className="rail__group">
            <span className="rail__head">{t.t("Translations")}</span>
            {others.length === 0 && <p className="tr-rail__none">{t.t("None yet.")}</p>}
            {others.map((l) => {
              const v = pct(graph, l, meta);
              return (
                <div key={l} className={"tr-lang" + (l === lang ? " is-active" : "")}>
                  <button type="button" className="tr-lang__pick" aria-current={l === lang ? "true" : undefined} onClick={() => setPick(l)}>
                    <LangCode lang={l} />
                    <span className="tr-lang__name">{languageName(l)}</span>
                    <span className="tr-pct">{v} %</span>
                    <Meter value={v} />
                  </button>
                  {!readOnly && (
                    <Button variant="ghost" size="icon" className="sm danger tr-lang__remove" aria-label={t.t("Remove {{lang}}", { lang: languageName(l) })} title={t.t("Remove {{lang}}", { lang: languageName(l) })} onClick={() => onRemove(l)}>
                      <Trash2 size={ICON.sm} />
                    </Button>
                  )}
                </div>
              );
            })}
            {!readOnly && addable.length > 0 && others.length > 0 && (
              adding ? (
                <div className="tr-add" role="group" aria-label={t.t("Add a language")}>
                  <div className="tr-add__head">
                    <span>{t.t("Add a language")}</span>
                    <Button variant="ghost" size="icon" className="sm" aria-label={t.t("Cancel")} onClick={() => setAdding(false)}>
                      <X size={ICON.sm} />
                    </Button>
                  </div>
                  {addable.map((l) => (
                    <button key={l} type="button" className="tr-add__opt" onClick={() => add(l)}>
                      <LangCode lang={l} />
                      {languageName(l)}
                    </button>
                  ))}
                </div>
              ) : (
                <Button variant="ghost" size="sm" className="tr-rail__add" icon={<Plus size={ICON.sm} />} onClick={() => setAdding(true)}>
                  {t.t("Add a language")}
                </Button>
              )
            )}
          </div>
        </aside>

        {!lang ? (
          <div className="tr-empty">
            <span className="tr-empty__icon" aria-hidden="true"><Languages size={28} /></span>
            <h3>{t.t("Reach teams in their own language")}</h3>
            <p>{t.t("Pick a language to translate into. You translate the texts only: steps, routes and answers stay as they are, and anything you leave empty shows in {{lang}}.", { lang: languageName(main) })}</p>
            {readOnly ? null : (
              <div className="tr-empty__langs">
                {addable.map((l) => (
                  <button key={l} type="button" className="tr-add__opt" onClick={() => add(l)}>
                    <LangCode lang={l} />
                    {languageName(l)}
                  </button>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="tr-work">
            <div className="tr-bar">
              <label className="tr-search">
                <Search size={ICON.sm} aria-hidden="true" />
                <input type="search" value={query} placeholder={t.t("Search the texts")} aria-label={t.t("Search the texts")} onChange={(e) => setQuery(e.target.value)} />
              </label>
              <div className="tr-filter" role="radiogroup" aria-label={t.t("Show")}>
                {filters.map((f) => (
                  <button key={f.id} type="button" role="radio" aria-checked={filter === f.id} className={filter === f.id ? "is-on" : ""} onClick={() => setFilter(f.id)}>
                    {f.label}
                    {f.n !== undefined && <span className="tr-filter__n">{f.n}</span>}
                  </button>
                ))}
              </div>
            </div>
            <div className="tr-cols" aria-hidden="true">
              <span><LangCode lang={main} /> {languageName(main)} · {t.t("original")}</span>
              <span><LangCode lang={lang} /> {languageName(lang)}</span>
            </div>
            <div className="tr-list" ref={list}>
              {groups.length === 0 ? (
                q ? (
                  <p className="tr-none">{t.t("No texts match “{{q}}”.", { q: query.trim() })}</p>
                ) : (
                  <div className="tr-none tr-none--done">
                    <CheckCircle2 size={28} aria-hidden="true" />
                    <strong>{filter === "outdated" ? t.t("Nothing is outdated.") : t.t("Everything is translated.")}</strong>
                    <Button size="sm" onClick={() => setFilter("all")}>{t.t("Show all texts")}</Button>
                  </div>
                )
              ) : (
                groups.map((g) => (
                  <section key={g.key} className="tr-group" aria-label={typeof g.title === "string" ? g.title : undefined}>
                    <h4 className="tr-group__head">
                      {g.icon}
                      <span>{g.title}</span>
                    </h4>
                    {g.texts.map((x) => (
                      <TextRow key={x.key} x={x} graph={graph} lang={lang} main={main} onChange={onChange} onKeyDown={next} {...(readOnly ? { readOnly } : {})} />
                    ))}
                    {g.files.map((f) => (
                      <FileRow key={f.key} f={f} graph={graph} lang={lang} onChange={onChange} {...(readOnly ? { readOnly } : {})} />
                    ))}
                  </section>
                ))
              )}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

/**
 * The original as it is now against the one a translation was made from:
 * added words green, removed red, and a word replaced yellow.
 */
function OriginalChanges({ was, now }: { was: string; now: string }) {
  const parts = wordDiff(was, now);
  const out: ReactNode[] = [];
  for (let i = 0; i < parts.length; i++) {
    const p = parts[i]!;
    const next = parts[i + 1];
    if (p.op === "del" && next?.op === "ins") {
      out.push(
        <span key={i} className="tr-diff__chg">
          <del>{p.text}</del>
          <ins>{next.text}</ins>
        </span>,
      );
      i++;
    } else if (p.op === "same") out.push(<span key={i}>{p.text}</span>);
    else if (p.op === "del") out.push(<del key={i} className="tr-diff__del">{p.text}</del>);
    else out.push(<ins key={i} className="tr-diff__ins">{p.text}</ins>);
  }
  return <>{out}</>;
}

/** One text: what it is and where it stands, the original, the translation. */
function TextRow({ x, graph, lang, main, onChange, onKeyDown, readOnly }: {
  x: Translatable;
  graph: Graph;
  lang: string;
  main: string;
  onChange: (g: Graph) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  readOnly?: boolean;
}) {
  const t = useT();
  const id = useId();
  const value = graph.translations?.[lang]?.[x.key] ?? "";
  const status = statusOf(graph, lang, x);
  const was = translationOriginal(graph, lang, x.key);
  const set = (v: string) => onChange(setTranslation(graph, lang, x.key, v, x.main));
  const STATUS: Record<Status, string> = {
    done: t.t("Translated"),
    todo: t.t("Not translated"),
    outdated: t.t("Outdated"),
    extra: x.answers ? t.t("Extra answers") : t.t("Optional"),
  };
  const field = {
    id,
    className: "tr-input",
    value,
    lang,
    disabled: readOnly,
    placeholder: x.answers ? t.t("More accepted answers, one per line") : t.t("Leave empty to show the original"),
    onKeyDown,
    onChange: (e: { target: { value: string } }) => set(e.target.value),
  };
  return (
    <div className={`tr-row is-${status}` + (x.answers && value.trim() ? " is-filled" : "")}>
      <div className="tr-row__meta">
        <span className="tr-status" role="img" aria-label={STATUS[status]} title={STATUS[status]}>
          {status === "done" || (status === "extra" && value.trim()) ? <Check size={ICON.xs} strokeWidth={3} aria-hidden="true" /> : null}
        </span>
        <label htmlFor={id}>{t.t(x.label, x.vars)}</label>
        {status === "outdated" && <span className="tr-tag tr-tag--warn">{t.t("Outdated")}</span>}
        {status === "extra" && <span className="tr-tag">{STATUS.extra}</span>}
        {!readOnly && (
          <div className="tr-row__tools">
            {status === "outdated" && (
              <Button variant="ghost" size="sm" icon={<Check size={ICON.sm} />} title={t.t("The original has changed since this was translated.")} onClick={() => set(value)}>
                {t.t("Still right")}
              </Button>
            )}
            {!x.answers && !value && (
              <Button variant="ghost" size="sm" className="tr-tool--quiet" icon={<ClipboardCopy size={ICON.sm} />} onClick={() => set(x.main)}>
                {t.t("Copy the original")}
              </Button>
            )}
          </div>
        )}
      </div>
      <div className="tr-row__src" lang={main}>
        {status === "outdated" && was !== undefined ? <OriginalChanges was={was} now={x.main} /> : x.main || <em>{t.t("(empty)")}</em>}
      </div>
      <div className="tr-row__dst">
        {x.multiline ? <textarea rows={Math.min(8, Math.max(2, x.main.split("\n").length))} {...field} /> : <input {...field} />}
        {status === "outdated" && <span className="tr-row__note">{t.t("Outdated: the original has changed since this was translated.")}</span>}
        {x.answers && <span className="tr-row__note">{t.t("A team answering in this language may give these too; the original answers still count.")}</span>}
      </div>
    </div>
  );
}

/** A picture, sound or film, as the row shows it. */
function FilePreview({ id, kind }: { id: number; kind: TranslatableFile["kind"] }) {
  const src = play.mediaURL(id);
  if (kind === "image") return <img className="tr-file__img" src={src} alt="" />;
  if (kind === "audio") return <audio className="tr-file__audio" controls preload="none" src={src} />;
  return <video className="tr-file__video" controls preload="metadata" src={src} />;
}

/** One file: the original's, and the one this language plays instead, if any. */
function FileRow({ f, graph, lang, onChange, readOnly }: { f: TranslatableFile; graph: Graph; lang: string; onChange: (g: Graph) => void; readOnly?: boolean }) {
  const t = useT();
  const input = useRef<HTMLInputElement>(null);
  const { busy, error, upload, ready } = useMediaUpload();
  const [wrong, setWrong] = useState(false);
  const own = translatedFile(graph, lang, f);
  const outdated = isFileOutdated(graph, lang, f);
  // An upload outlives the render that started it: set it on the graph as it is when it lands.
  const latest = useRef({ graph, onChange });
  latest.current = { graph, onChange };
  const set = (id: number | undefined) => onChange(setTranslatedFile(graph, lang, f, id));
  const WHAT = { image: t.t("Pick a picture."), audio: t.t("Pick a sound."), video: t.t("Pick a film.") };

  const pick = async (files: FileList | null) => {
    setWrong(false);
    const [added] = await upload(files);
    if (input.current) input.current.value = "";
    if (!added) return;
    if (added.kind !== f.kind) return setWrong(true);
    const now = latest.current;
    now.onChange(setTranslatedFile(now.graph, lang, f, added.id));
  };

  return (
    <div className={"tr-row tr-file" + (outdated ? " is-outdated" : " is-extra") + (own ? " is-filled" : "")}>
      <div className="tr-row__meta">
        <span className="tr-status" role="img" aria-label={own ? t.t("Own file") : t.t("Plays the original")} title={own ? t.t("Own file") : t.t("Plays the original")}>
          {own ? <Check size={ICON.xs} strokeWidth={3} aria-hidden="true" /> : null}
        </span>
        <span>{t.t(f.label, f.vars)}</span>
        {outdated ? <span className="tr-tag tr-tag--warn">{t.t("Outdated")}</span> : <span className="tr-tag">{t.t("Optional")}</span>}
        {!readOnly && (
          <div className="tr-row__tools">
            {outdated && (
              <Button variant="ghost" size="sm" icon={<Check size={ICON.sm} />} title={t.t("The original file has been replaced since this one was added.")} onClick={() => set(own)}>
                {t.t("Still right")}
              </Button>
            )}
            {own !== undefined && (
              <Button variant="ghost" size="sm" className="tr-tool--quiet" icon={<Undo2 size={ICON.sm} />} onClick={() => set(undefined)}>
                {t.t("Play the original")}
              </Button>
            )}
          </div>
        )}
      </div>
      <div className="tr-row__src">
        <FilePreview id={f.main} kind={f.kind} />
      </div>
      <div className="tr-row__dst">
        {own !== undefined ? <FilePreview id={own} kind={f.kind} /> : <span className="tr-row__note">{t.t("Plays the original.")}</span>}
        {!readOnly && (
          <div>
            <input ref={input} type="file" hidden accept={`${f.kind}/*`} onChange={(e) => void pick(e.target.files)} />
            <Button size="sm" icon={<Upload size={ICON.sm} />} disabled={busy || !ready} onClick={() => input.current?.click()}>
              {busy ? t.t("Uploading…") : own !== undefined ? t.t("Replace the file") : t.t("Add a file in this language")}
            </Button>
          </div>
        )}
        {(error || wrong) && <div className="alert alert--danger">{wrong ? WHAT[f.kind] : error}</div>}
        {outdated && <span className="tr-row__note">{t.t("Outdated: the original file has been replaced since this one was added.")}</span>}
      </div>
    </div>
  );
}
