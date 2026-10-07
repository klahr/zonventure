import { useState } from "react";
import { ChevronRight, GraduationCap, PartyPopper, Workflow } from "lucide-react";
import { ApiError, request } from "@zonventure/api";
import { TEMPLATE_THEMES, TemplatePicker, themeById, type TemplateInfo } from "@zonventure/trail";
import { useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import { Dialog, ErrorBox, Field, Loading, useLoad } from "../../ui/index.ts";
import "@zonventure/trail/trail.css";

/**
 * "New adventure" asks which editor first: a quiz trail in the simple
 * creator — cards in a row, done in minutes — or the full designer. A
 * quiz trail starts from a template, in Swedish or English.
 */
export function NewKindDialog({ onDesigner, onTutorial, onClose }: { onDesigner: () => void; onTutorial: () => void; onClose: () => void }) {
  const t = useT();
  const [kind, setKind] = useState<"choose" | "trail">("choose");
  const [lang, setLang] = useState(t.current() === "sv" ? "sv" : "en");
  const templates = useLoad(
    (signal) => (kind === "trail" ? request<{ templates: TemplateInfo[] }>(`/api/admin/trail-templates?lang=${lang}`, { signal }).then((r) => r.templates) : Promise.resolve([])),
    [kind, lang],
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = async (template: string) => {
    setBusy(true);
    setError(null);
    try {
      const r = await request<{ slug: string }>("/api/admin/trails", { method: "POST", body: { template, language: lang } });
      // Each template starts in a look of its own; changed in the editor.
      const th = themeById(TEMPLATE_THEMES[template]);
      if (th) await request(`/api/admin/adventures/${encodeURIComponent(r.slug)}/cards/theme`, { method: "PUT", body: { id: th.id, look: th.look } }).catch(() => {});
      onClose();
      navigate({ name: "trail", slug: r.slug });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setBusy(false);
    }
  };

  return (
    <Dialog title={t.t("New adventure")} wide onClose={() => !busy && onClose()}>
      {kind === "choose" ? (
        <ul className="trail-templates">
          <li>
            <button type="button" className="trail-template trail-template--birthday" onClick={() => setKind("trail")}>
              <span className="trail-template__art" aria-hidden="true">
                <PartyPopper size={26} />
              </span>
              <span className="trail-template__body">
                <strong>{t.t("Party adventure (simple)")}</strong>
                <span>{t.t("A little adventure for a party: questions on cards you print and hide, and teams racing with their phones. Made in minutes, on a phone too.")}</span>
              </span>
              <span className="trail-template__go" aria-hidden="true">
                <ChevronRight size={18} />
              </span>
            </button>
          </li>
          <li>
            <button type="button" className="trail-template trail-template--office" onClick={onDesigner}>
              <span className="trail-template__art" aria-hidden="true">
                <Workflow size={26} />
              </span>
              <span className="trail-template__body">
                <strong>{t.t("Designer (full)")}</strong>
                <span>{t.t("Places on a map, puzzles of every kind, branches and story. The full toolbox.")}</span>
              </span>
              <span className="trail-template__go" aria-hidden="true">
                <ChevronRight size={18} />
              </span>
            </button>
          </li>
          <li>
            <button type="button" className="trail-template trail-template--family" onClick={onTutorial}>
              <span className="trail-template__art" aria-hidden="true">
                <GraduationCap size={26} />
              </span>
              <span className="trail-template__body">
                <strong>{t.t("Tutorial: your first adventure")}</strong>
                <span>{t.t("Learn the designer by building a short adventure, Erik's stolen elephants, with a guide beside you. About 30 minutes.")}</span>
              </span>
              <span className="trail-template__go" aria-hidden="true">
                <ChevronRight size={18} />
              </span>
            </button>
          </li>
        </ul>
      ) : (
        <>
          <Field label={t.t("Language")}>
            <select value={lang} onChange={(e) => setLang(e.target.value)} disabled={busy}>
              <option value="sv">Svenska</option>
              <option value="en">English</option>
            </select>
          </Field>
          <p className="evs-muted">{t.t("Start from an example and make it yours.")}</p>
          {templates.error ? (
            <ErrorBox message={templates.error} retry={templates.reload} retryLabel={t.t("Retry")} />
          ) : !templates.data ? (
            <Loading label={t.t("Loading…")} />
          ) : (
            <TemplatePicker i18n={t} templates={templates.data} busy={busy} onPick={(name) => void create(name)} />
          )}
          {error && <ErrorBox message={error} />}
        </>
      )}
    </Dialog>
  );
}
