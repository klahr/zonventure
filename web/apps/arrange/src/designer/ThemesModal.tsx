import { createContext, useState, type CSSProperties } from "react";
import { adventures, type play } from "@zonventure/api";
import type { Envelope, Graph, Theme, ThemeLook } from "@zonventure/graph";
import { brandVars } from "@zonventure/play-ui";
import { Building2, Copy, Plus, Trash2 } from "lucide-react";
import { useT } from "../i18n.tsx";
import { useSession } from "../session.tsx";
import { LookControls } from "./Look.tsx";
import { LookPreview } from "./LookPreview.tsx";
import { StarterGallery } from "./StarterGallery.tsx";
import { PanelModal } from "./dz/dialogs.tsx";
import { ConfirmModal, ICON } from "./dz/ui.tsx";


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
  const [sharing, setSharing] = useState<{ theme: Theme; existing: adventures.OrgTheme } | null>(null);
  const [shared, setShared] = useState<{ slug: string; text: string } | null>(null);
  const { state } = useSession();
  const orgAdmin = state.status === "authenticated" && state.user.role === "org_admin";
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

  // To the organisation's themes, for its other adventures: under the
  // theme's name, replacing a library theme of that name when agreed.
  const share = async (th: Theme, replace?: adventures.OrgTheme) => {
    const name = th.name.trim() || t.t("Untitled theme");
    const look = th.look as play.Brand;
    try {
      let dropped: boolean;
      if (replace) dropped = (await adventures.updateOrgTheme(replace.id, name, look)).dropped_media;
      else {
        const existing = (await adventures.listOrgThemes()).find((o) => o.name.toLowerCase() === name.toLowerCase());
        if (existing) return setSharing({ theme: th, existing });
        dropped = (await adventures.createOrgTheme(name, look)).dropped_media;
      }
      setShared({
        slug: th.slug,
        text: dropped
          ? t.t("Saved to the organisation's themes, without its picture and sounds: those belong to this adventure.")
          : t.t("Saved to the organisation's themes. Its other adventures can start a theme from it."),
      });
    } catch (e) {
      setShared({ slug: th.slug, text: e instanceof Error ? e.message : String(e) });
    }
  };

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
            manage
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
                {orgAdmin && (
                  <button type="button" className="btn btn--ghost btn--sm" title={t.t("Keep this theme for all the organisation's adventures")} onClick={() => void share(theme)}>
                    <Building2 size={ICON.sm} aria-hidden />
                    {t.t("Save for the organisation")}
                  </button>
                )}
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
              {shared?.slug === theme.slug && (
                <p className="field__hint" role="status">
                  {shared.text}
                </p>
              )}
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
      {sharing && (
        <ConfirmModal
          title={t.t("Replace the organisation's “{{name}}”?", { name: sharing.existing.name })}
          message={t.t("The organisation already has a theme of that name. Adventures that use it keep their copy.")}
          confirmLabel={t.t("Replace")}
          onConfirm={() => {
            const { theme: th, existing } = sharing;
            setSharing(null);
            void share(th, existing);
          }}
          onCancel={() => setSharing(null)}
        />
      )}
    </PanelModal>
  );
}
