import { createContext, useEffect, useState, type CSSProperties } from "react";
import type { Envelope, Graph, Theme, ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, brandVars, loadFont } from "@zonventure/play-ui";
import { Copy, Plus, Trash2 } from "lucide-react";
import { useT } from "../i18n.tsx";
import { LookControls } from "./Look.tsx";
import { LookPreview } from "./LookPreview.tsx";
import { STARTER_GROUPS, STARTER_THEMES } from "./starterThemes.ts";
import { PanelModal } from "./dz/dialogs.tsx";
import { ConfirmModal, ICON } from "./dz/ui.tsx";

/**
 * What a new theme can start from, large enough to choose by eye: each
 * look on a small page with a title, a text card and an answer button,
 * in its own colours, fonts, background and cards.
 */
function StarterGallery({ onPick, onCancel }: { onPick: (name: string, look: ThemeLook) => void; onCancel: () => void }) {
  const t = useT();
  const starters = [{ id: "default", label: "Default look", group: "", look: DEFAULT_LOOK as ThemeLook }, ...STARTER_THEMES];
  useEffect(() => {
    for (const s of starters) {
      loadFont(s.look.font);
      loadFont(s.look.heading_font);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <section className="starters" aria-label={t.t("Start from")}>
      <div className="starters__head">
        <h4>{t.t("Start from")}</h4>
        <button type="button" className="btn btn--ghost btn--sm" onClick={onCancel}>{t.t("Cancel")}</button>
      </div>
      {[{ id: "", label: "", items: [starters[0]!] }, ...STARTER_GROUPS.map((g) => ({ ...g, items: STARTER_THEMES.filter((s) => s.group === g.id) }))].map((g) => (
        <div key={g.id || "default"} className="starters__group">
          {g.label && <h5 className="starters__group-title">{t.t(g.label)}</h5>}
          <div className="starters__grid">
            {g.items.map((s) => (
              <button key={s.id} type="button" className="starters__pick" onClick={() => onPick(s.id === "default" ? "" : t.t(s.label), s.look)}>
                <span className="starters__page pv" data-theme="light" style={brandVars(s.look) as CSSProperties}>
                  <span className="starters__title">{t.t("Erik's stolen elephants")}</span>
                  <span className="starters__card">{t.t("Hi! Someone has stolen my elephants. Can you help me find them?")}</span>
                  <span className="starters__btn">{t.t("Answer")}</span>
                </span>
                <span className="starters__name">{t.t(s.label)}</span>
              </button>
            ))}
          </div>
        </div>
      ))}
      <p className="field__hint">{t.t("To start from the adventure's own look, use Save this look as a theme under Adventure settings.")}</p>
    </section>
  );
}

/** Opens the theme editor, on a theme if given: for the theme step's inspector. */
export const OpenThemes = createContext<((theme?: string) => void) | null>(null);

/** theme-1, theme-2, … — never a built-in theme's name, never one in use. */
function freshSlug(themes: Theme[]): string {
  const taken = new Set(themes.map((th) => th.slug));
  for (let i = 1; ; i++) if (!taken.has(`theme-${i}`)) return `theme-${i}`;
}

/** How many theme effects switch to this theme. */
export function themeUses(g: Graph, slug: string): number {
  return g.nodes.filter((n) => n.type === "effect" && n.effect?.kind === "theme" && n.effect.theme === slug).length;
}

interface Props {
  slug: string;
  graph: Graph;
  envelope: Envelope;
  title: string;
  /** The designer's selected step, previewed first. */
  selected: string | null;
  /** The theme to open on. */
  initial?: string | null;
  onChange: (g: Graph) => void;
  onClose: () => void;
}

/**
 * The adventure's saved themes, edited with the same controls as its
 * main look and previewed on a phone. Themes live in the graph: every
 * change is a draft edit — autosaved, undoable, published with the rest —
 * so a running event keeps the themes its steps switch to.
 */
export function ThemesModal({ slug, graph, envelope, title, selected, initial, onChange, onClose }: Props) {
  const t = useT();
  const themes = graph.themes ?? [];
  const [current, setCurrent] = useState<string | null>(initial ?? themes[0]?.slug ?? null);
  const [deleting, setDeleting] = useState<Theme | null>(null);
  const [choosing, setChoosing] = useState(false);
  const theme = themes.find((th) => th.slug === current) ?? null;

  const save = (next: Theme[]) => {
    const g = { ...graph };
    if (next.length) g.themes = next;
    else delete g.themes;
    onChange(g);
  };
  const add = (name: string, look: ThemeLook) => {
    const th: Theme = { slug: freshSlug(themes), name, look: structuredClone(look) };
    save([...themes, th]);
    setCurrent(th.slug);
    setChoosing(false);
  };
  const update = (patch: Partial<Theme>) => theme && save(themes.map((th) => (th.slug === theme.slug ? { ...th, ...patch } : th)));
  const remove = (s: string) => {
    const next = themes.filter((th) => th.slug !== s);
    save(next);
    setCurrent(next[0]?.slug ?? null);
  };
  const uses = theme ? themeUses(graph, theme.slug) : 0;

  return (
    <PanelModal title={t.t("Themes")} wide onClose={onClose}>
      <div className={"themes" + (choosing ? " themes--choosing" : theme ? "" : " themes--empty")}>
        <nav className="themes__list" aria-label={t.t("Saved themes")}>
          {themes.map((th) => (
            <button key={th.slug} type="button" className={"themes__item" + (th.slug === current ? " is-on" : "")} aria-current={th.slug === current} onClick={() => setCurrent(th.slug)}>
              <span className="themes__swatch look__thumb" style={brandVars(th.look) as CSSProperties} />
              <span className="themes__name">{th.name || t.t("Untitled theme")}</span>
            </button>
          ))}
          <button type="button" className={"btn btn--ghost btn--sm" + (choosing ? " is-on" : "")} aria-pressed={choosing} onClick={() => setChoosing((c) => !c)}>
            <Plus size={ICON.sm} aria-hidden />
            {t.t("New theme")}
          </button>
        </nav>

        {choosing ? (
          <StarterGallery
            onPick={(name, look) => add(name || t.t("Theme {{n}}", { n: themes.length + 1 }), look)}
            onCancel={() => setChoosing(false)}
          />
        ) : theme ? (
          <>
            <div className="themes__editor look__controls">
              <label className="field">
                <span className="field__label">{t.t("Name")}</span>
                <input className="field__input" value={theme.name} maxLength={80} onChange={(e) => update({ name: e.target.value })} />
              </label>
              <div className="look__actions">
                <button type="button" className="btn btn--ghost btn--sm" onClick={() => add(t.t("{{name}} (copy)", { name: theme.name }), theme.look)}>
                  <Copy size={ICON.sm} aria-hidden />
                  {t.t("Duplicate")}
                </button>
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  disabled={uses > 0}
                  title={uses > 0 ? t.t("Steps still switch to this theme. Change them first.") : undefined}
                  onClick={() => setDeleting(theme)}
                >
                  <Trash2 size={ICON.sm} aria-hidden />
                  {t.t("Delete")}
                </button>
              </div>
              <p className="field__hint">
                {uses > 0
                  ? t.plural(uses, { one: t.t("{{n}} step switches to this theme."), other: t.t("{{n}} steps switch to this theme.") })
                  : t.t("No step uses it yet. Add a Change the theme step, or use it as the main look under Adventure settings.")}
              </p>
              <LookControls brand={theme.look} onChange={(look) => update({ look })} />
              <p className="field__hint">{t.t("Themes are part of the draft: changes save with it and reach players when you publish.")}</p>
            </div>
            <aside className="look__preview">
              <LookPreview slug={slug} graph={graph} envelope={envelope} brand={theme.look} title={title} selected={selected} />
            </aside>
          </>
        ) : (
          <p className="themes__none field__hint">{t.t("No saved themes yet. Make one to switch to it from a step, or to swap the adventure's style in one go.")}</p>
        )}
      </div>
      {deleting && (
        <ConfirmModal
          title={t.t("Delete the theme “{{name}}”?", { name: deleting.name || t.t("Untitled theme") })}
          message={t.t("No step uses it. Undo brings it back.")}
          confirmLabel={t.t("Delete theme")}
          danger
          onConfirm={() => {
            remove(deleting.slug);
            setDeleting(null);
          }}
          onCancel={() => setDeleting(null)}
        />
      )}
    </PanelModal>
  );
}
