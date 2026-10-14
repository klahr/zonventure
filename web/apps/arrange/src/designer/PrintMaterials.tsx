import { useContext, useRef, useState } from "react";
import { ApiError, adventures, play } from "@zonventure/api";
import { MAX_PRINT_MATERIALS, type Delivery, type Graph, type PrintMaterial } from "@zonventure/graph";
import { FileText, Plus, Trash2 } from "lucide-react";
import { useT } from "../i18n.tsx";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { Button, ICON } from "./dz/ui.tsx";
import { AdventureSlug } from "./MediaEditor.tsx";
import { languageName, translationLanguages } from "./Translate.tsx";

/**
 * Adventure settings: the PDFs that come with the adventure on paper, one
 * per language it is played in. They are part of the graph, so a
 * published version keeps the ones it was published with; an upload is
 * stored at once and saved into the draft with Save, like a picture.
 */
export function PrintMaterials({ graph, main, onChange }: { graph: Graph; main: string; onChange: (g: Graph) => void }) {
  const t = useT();
  const materials = graph.print_materials ?? [];
  const langs = [main, ...translationLanguages(graph, main)];
  const set = (next: PrintMaterial[]) => {
    if (next.length) {
      onChange({ ...graph, print_materials: next });
    } else {
      const g = { ...graph };
      delete g.print_materials;
      onChange(g);
    }
  };
  const update = (i: number, pm: PrintMaterial) => set(materials.map((x, j) => (j === i ? pm : x)));
  const nextID = () => {
    const used = new Set(materials.map((m) => m.id));
    for (let i = 1; ; i++) if (!used.has(`print-${i}`)) return `print-${i}`;
  };
  const deliveries: { value: Delivery; label: string }[] = [
    { value: "posted", label: t.t("Posted by you") },
    { value: "emailed", label: t.t("Emailed to the buyer") },
    { value: "organiser", label: t.t("For organisers only") },
  ];

  return (
    <section className="insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.print} />
        {t.t("Print materials")}
      </h3>
      <p className="field__hint">
        {t.t("PDFs that come with the adventure on paper, such as a letter with the first clue. Players never see them in the game. A posted one needs a PDF in every language the adventure is played in.")}
      </p>
      {materials.map((pm, i) => (
        <div key={pm.id} className="box">
          <div className="box__row">
            <input className="field__input" value={pm.name} placeholder={t.t("Name, such as Letter")} aria-label={t.t("Name")}
              onChange={(e) => update(i, { ...pm, name: e.target.value })} />
            <select className="field__input print__delivery" value={pm.delivery} aria-label={t.t("Delivery")}
              onChange={(e) => update(i, { ...pm, delivery: e.target.value as Delivery })}>
              {deliveries.map((d) => <option key={d.value} value={d.value}>{d.label}</option>)}
            </select>
            <Button variant="ghost" size="icon" className="danger" onClick={() => set(materials.filter((_, j) => j !== i))} aria-label={t.t("Remove")} title={t.t("Remove")}>
              <Trash2 size={ICON.sm} />
            </Button>
          </div>
          {langs.map((l) => (
            <PrintFile
              key={l}
              lang={l}
              id={pm.files?.[l]}
              required={pm.delivery === "posted"}
              onChange={(id) => {
                const files = { ...(pm.files ?? {}) };
                if (id) files[l] = id;
                else delete files[l];
                const next: PrintMaterial = { ...pm, files };
                if (!Object.keys(files).length) delete next.files;
                update(i, next);
              }}
            />
          ))}
        </div>
      ))}
      {materials.length < MAX_PRINT_MATERIALS && (
        <button type="button" className="add-btn" onClick={() => set([...materials, { id: nextID(), name: "", delivery: "posted" }])}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add print material")}
        </button>
      )}
    </section>
  );
}

/** One language's PDF of a print material: open it, replace it, remove it. */
function PrintFile({ lang, id, required, onChange }: { lang: string; id: number | undefined; required: boolean; onChange: (id: number | undefined) => void }) {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pick = async (file: File | undefined) => {
    if (input.current) input.current.value = "";
    if (!file) return;
    setBusy(true);
    setError(null);
    try {
      const r = await adventures.uploadMedia(slug, file);
      if (r.kind !== "pdf") setError(t.t("That isn't a PDF."));
      else onChange(r.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="box__row print__file">
      <span className="print__lang">{languageName(lang)}</span>
      {id ? (
        <a className="btn link" href={play.mediaURL(id)} target="_blank" rel="noreferrer">
          <FileText size={ICON.sm} aria-hidden="true" /> {t.t("Open PDF")}
        </a>
      ) : (
        <span className={required ? "field__error" : "field__hint"}>{required ? t.t("Missing: needed to post it") : t.t("No PDF")}</span>
      )}
      <input ref={input} type="file" accept="application/pdf" hidden onChange={(e) => void pick(e.target.files?.[0])} />
      <Button variant="link" disabled={busy || !slug} onClick={() => input.current?.click()}>
        {busy ? t.t("Uploading…") : id ? t.t("Replace") : t.t("Upload PDF")}
      </Button>
      {id && <Button variant="link" onClick={() => onChange(undefined)}>{t.t("Remove")}</Button>}
      {error && <span className="field__error" role="alert">{error}</span>}
    </div>
  );
}
