import { useEffect, type CSSProperties } from "react";
import type { ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, brandVars, loadFont } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { STARTER_GROUPS, STARTER_THEMES } from "./starterThemes.ts";
import "./starters.css";

/**
 * What a new theme can start from, large enough to choose by eye: each
 * look on a small page with a title, a text card and an answer button,
 * in its own colours, fonts, background and cards.
 */
export function StarterGallery({ onPick, onCancel, selected, sample }: {
  onPick: (name: string, look: ThemeLook, id: string) => void;
  onCancel?: () => void;
  /** The starter picked so far, marked. */
  selected?: string;
  /** The title and text on each sample page; the adventure's own when known. */
  sample?: { title: string; text: string };
}) {
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
        {onCancel && <button type="button" className="btn btn--ghost btn--sm" onClick={onCancel}>{t.t("Cancel")}</button>}
      </div>
      {[{ id: "", label: "", items: [starters[0]!] }, ...STARTER_GROUPS.map((g) => ({ ...g, items: STARTER_THEMES.filter((s) => s.group === g.id) }))].map((g) => (
        <div key={g.id || "default"} className="starters__group">
          {g.label && <h5 className="starters__group-title">{t.t(g.label)}</h5>}
          <div className="starters__grid">
            {g.items.map((s) => (
              <button key={s.id} type="button" className={"starters__pick" + (selected === s.id ? " is-on" : "")} aria-pressed={selected === s.id} onClick={() => onPick(s.id === "default" ? "" : t.t(s.label), s.look, s.id)}>
                <span className="starters__page pv" data-theme="light" style={brandVars(s.look) as CSSProperties}>
                  <span className="starters__title">{sample?.title || t.t("Erik's stolen elephants")}</span>
                  <span className="starters__card">{sample?.text || t.t("Hi! Someone has stolen my elephants. Can you help me find them?")}</span>
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
