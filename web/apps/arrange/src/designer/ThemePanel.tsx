import { useState, type CSSProperties } from "react";
import type { play } from "@zonventure/api";
import type { Graph, Theme, ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, brandVars, loadFont } from "@zonventure/play-ui";
import { Plus, Star } from "lucide-react";
import { useT } from "../i18n.tsx";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { Button, ICON } from "./dz/ui.tsx";
import { PanelModal } from "./dz/dialogs.tsx";
import { SamplePreview } from "./SamplePreview.tsx";
import { StarterGallery, canon } from "./StarterGallery.tsx";
import { freshSlug, themeUses } from "./ThemesModal.tsx";

interface Props {
  graph: Graph;
  /** The adventure's look as set outside the graph: the main look until the graph has a main theme. */
  brand: play.Brand;
  /** Absent when read-only. */
  onChange?: (g: Graph) => void;
  /** Open the theme editor on a theme. */
  onEdit: (slug: string) => void;
}

/**
 * The adventure's themes, one of them the main theme: what players see
 * wherever no step changes the theme. Themes live in the draft, so a
 * change reaches players when the adventure is published. An adventure
 * without a main theme shows its own look as one, made a theme of the
 * draft on the first change.
 */
export function ThemePanel({ graph, brand, onChange, onEdit }: Props) {
  const t = useT();
  const [browsing, setBrowsing] = useState(false);
  const themes = graph.themes ?? [];
  const main = themes.find((th) => th.slug === graph.main_theme);
  const stand: Theme | null = main ? null : { slug: themes.some((x) => x.slug === "main") ? freshSlug(themes) : "main", name: t.t("Main theme"), look: structuredClone(brand) as ThemeLook };
  const rows = stand ? [stand, ...themes] : themes;
  const mainSlug = main?.slug ?? stand!.slug;
  const mainLook = (main?.look ?? brand) as play.Brand;
  const empty = (look: ThemeLook) => canon(look) === canon({});

  const edit = (slug: string) => {
    if (stand && slug === stand.slug) onChange?.({ ...graph, themes: [...themes, stand], main_theme: stand.slug });
    onEdit(slug);
  };
  const add = (name: string, look: ThemeLook) => {
    setBrowsing(false);
    if (!onChange) return;
    // A first pick replaces a main theme nobody has made anything of yet.
    const blank = main ? themes.length === 1 && empty(main.look) && themeUses(graph, main.slug) === 0 : themes.length === 0 && empty(stand!.look);
    if (blank) {
      const th: Theme = { slug: mainSlug, name, look: structuredClone(look) };
      onChange({ ...graph, themes: [th], main_theme: th.slug });
      return;
    }
    const base = stand ? [...themes, stand] : themes;
    const th: Theme = { slug: freshSlug(base), name, look: structuredClone(look) };
    onChange({ ...graph, themes: [...base, th], main_theme: mainSlug });
    onEdit(th.slug);
  };
  const makeMain = (slug: string) => onChange?.({ ...graph, main_theme: slug });

  loadFont(mainLook.heading_font ?? mainLook.font);
  return (
    <section className="look insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.look} />
        {t.t("Theme")}
      </h3>
      <div className="look__body">
        <div className="look__controls">
          <ul className="look__themes">
            {rows.map((th) => {
              const isMain = th.slug === mainSlug;
              const uses = themeUses(graph, th.slug);
              const look = (empty(th.look) ? DEFAULT_LOOK : th.look) as play.Brand;
              return (
                <li key={th.slug} className={isMain ? "look__theme look__main" : "look__theme"}>
                  <span className="look__main-thumb look__thumb" style={brandVars(look) as CSSProperties} aria-hidden>
                    <span className="theme-strip__aa">Aa</span>
                    <span className="theme-strip__dot" />
                  </span>
                  <span className="look__main-name">
                    {th.name || t.t("Untitled theme")}
                    <span className="field__hint">
                      {isMain ? t.t("Main theme") : uses > 0 ? t.plural(uses, { one: t.t("Used by {{n}} step"), other: t.t("Used by {{n}} steps") }) : t.t("Not used yet")}
                    </span>
                  </span>
                  {isMain ? (
                    <Star size={ICON.sm} className="look__star" aria-label={t.t("Main theme")} />
                  ) : (
                    onChange && <Button size="sm" onClick={() => makeMain(th.slug)}>{t.t("Make main")}</Button>
                  )}
                  {onChange && <Button size="sm" onClick={() => edit(th.slug)}>{t.t("Edit")}</Button>}
                </li>
              );
            })}
          </ul>
          {onChange && (
            <div className="look__actions">
              <Button size="sm" className="look__add" onClick={() => setBrowsing(true)}>
                <Plus size={ICON.sm} aria-hidden />
                {t.t("Add theme…")}
              </Button>
            </div>
          )}
          <p className="field__hint">
            {t.t("Players see the main theme wherever no step changes the theme. Changes to themes reach players when you publish.")}
          </p>
        </div>
        <div className="look__preview">
          <SamplePreview brand={(empty(mainLook as ThemeLook) ? DEFAULT_LOOK : mainLook) as play.Brand} />
        </div>
      </div>
      {browsing && (
        <PanelModal title={t.t("Choose a theme")} wide onClose={() => setBrowsing(false)}>
          <StarterGallery heading="" onPick={(name, look) => add(name || t.t("Theme {{n}}", { n: rows.length + 1 }), look)} />
        </PanelModal>
      )}
    </section>
  );
}
