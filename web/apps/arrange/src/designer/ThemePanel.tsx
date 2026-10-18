import { useState, type CSSProperties } from "react";
import type { play } from "@zonventure/api";
import type { Graph, Theme, ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, brandVars, loadFont } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { ConceptIcon, RESOURCE } from "./dz/concepts.tsx";
import { Button } from "./dz/ui.tsx";
import { PanelModal } from "./dz/dialogs.tsx";
import { SamplePreview } from "./SamplePreview.tsx";
import { StarterGallery, canon, starterOf } from "./StarterGallery.tsx";
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
 * The adventure's theme: what players see wherever no step changes it.
 * Changing it picks another look in its place; customizing edits it.
 * Themes live in the draft, so either reaches players when the adventure
 * is published. Below, the themes steps switch to, if any. An adventure
 * without a main theme shows its own look as one, made a theme of the
 * draft on the first change.
 */
export function ThemePanel({ graph, brand, onChange, onEdit }: Props) {
  const t = useT();
  const [browsing, setBrowsing] = useState(false);
  const themes = graph.themes ?? [];
  const main: Theme = themes.find((th) => th.slug === graph.main_theme) ?? {
    slug: themes.some((x) => x.slug === "main") ? freshSlug(themes) : "main",
    name: t.t("Main theme"),
    look: structuredClone(brand) as ThemeLook,
  };
  const others = themes.filter((th) => th.slug !== main.slug);
  const shown = (look: ThemeLook) => (canon(look) === canon({}) ? DEFAULT_LOOK : look) as play.Brand;

  // The main theme replaced (or made, for an adventure without one).
  const put = (th: Theme) => onChange?.({ ...graph, themes: [...others, th], main_theme: th.slug });
  const change = (name: string, look: ThemeLook, id: string) => {
    setBrowsing(false);
    const starter = starterOf(id, look);
    // The default theme is no look of its own: the organisation's shows.
    const own = id === "default" ? {} : structuredClone(look);
    put({ slug: main.slug, name: name || t.t("Default theme"), look: own, ...(starter ? { starter } : {}) });
  };
  const customize = () => {
    if (!themes.some((th) => th.slug === main.slug)) put(main);
    onEdit(main.slug);
  };

  const look = shown(main.look);
  loadFont(look.heading_font ?? look.font);
  return (
    <section className="look insp__section">
      <h3>
        <ConceptIcon {...RESOURCE.look} />
        {t.t("Theme")}
      </h3>
      <div className="look__body">
        <div className="look__controls">
          <div className="look__theme look__main">
            <span className="look__main-thumb look__thumb" style={brandVars(look) as CSSProperties} aria-hidden>
              <span className="theme-strip__aa">Aa</span>
              <span className="theme-strip__dot" />
            </span>
            <span className="look__main-name">{main.name || t.t("Untitled theme")}</span>
          </div>
          {onChange && (
            <div className="look__actions">
              <Button size="sm" className="look__change" onClick={() => setBrowsing(true)}>{t.t("Change theme…")}</Button>
              <Button size="sm" onClick={customize}>{t.t("Customize…")}</Button>
            </div>
          )}
          <p className="field__hint">{t.t("Players see this theme wherever no step changes it. Changes reach players when you publish.")}</p>
          {others.length > 0 && (
            <div className="field">
              <span className="field__label">{t.t("Themes steps switch to")}</span>
              <ul className="look__themes">
                {others.map((th) => {
                  const uses = themeUses(graph, th.slug);
                  return (
                    <li key={th.slug} className="look__theme">
                      <span className="look__main-thumb look__thumb" style={brandVars(shown(th.look)) as CSSProperties} aria-hidden>
                        <span className="theme-strip__aa">Aa</span>
                        <span className="theme-strip__dot" />
                      </span>
                      <span className="look__main-name">
                        {th.name || t.t("Untitled theme")}
                        <span className="field__hint">
                          {uses > 0 ? t.plural(uses, { one: t.t("Used by {{n}} step"), other: t.t("Used by {{n}} steps") }) : t.t("Not used yet")}
                        </span>
                      </span>
                      {onChange && <Button size="sm" onClick={() => onEdit(th.slug)}>{t.t("Edit")}</Button>}
                    </li>
                  );
                })}
              </ul>
            </div>
          )}
        </div>
        <div className="look__preview">
          <SamplePreview brand={look} />
        </div>
      </div>
      {browsing && (
        <PanelModal title={t.t("Choose a theme")} wide onClose={() => setBrowsing(false)}>
          <StarterGallery heading="" selected={main.starter ?? (canon(main.look) === canon({}) ? "default" : undefined)} onPick={change} />
        </PanelModal>
      )}
    </section>
  );
}
