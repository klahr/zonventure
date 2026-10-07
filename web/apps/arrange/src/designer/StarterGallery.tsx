import { useEffect, useState, type CSSProperties } from "react";
import { adventures } from "@zonventure/api";
import type { ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, brandVars, loadFont } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { STARTER_GROUPS, STARTER_THEMES } from "@zonventure/play-ui";
import "./starters.css";

/**
 * What a new theme can start from, large enough to choose by eye: each
 * look on a small page with a title, a text card and an answer button,
 * in its own colours, fonts, background and cards. The organisation's
 * own themes come first, after the default.
 */
export function StarterGallery({ onPick, onCancel, selected, sample, ownLookHint = true }: {
  onPick: (name: string, look: ThemeLook, id: string) => void;
  onCancel?: () => void;
  /** The starter picked so far, marked. */
  selected?: string;
  /** The title and text on each sample page; the adventure's own when known. */
  sample?: { title: string; text: string };
  /** Point to starting from the adventure's own look; off outside an adventure. */
  ownLookHint?: boolean;
}) {
  const t = useT();
  const [library, setLibrary] = useState<adventures.OrgTheme[]>([]);
  const starters = [{ id: "default", label: "Default look", group: "", look: DEFAULT_LOOK as ThemeLook }, ...STARTER_THEMES];
  const own = library.map((th) => ({ id: `org-${th.id}`, label: th.name, own: th, look: th.look as ThemeLook }));
  useEffect(() => {
    for (const s of starters) {
      loadFont(s.look.font);
      loadFont(s.look.heading_font);
    }
    const ac = new AbortController();
    // Without its library the gallery still offers the starters.
    adventures.listOrgThemes(ac.signal).then((themes) => {
      setLibrary(themes);
      for (const th of themes) {
        loadFont(th.look.font);
        loadFont(th.look.heading_font);
      }
    }, () => {});
    return () => ac.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const groups: { id: string; label: string; items: { id: string; label: string; look: ThemeLook; own?: adventures.OrgTheme }[] }[] = [
    { id: "", label: "", items: [starters[0]!] },
    ...(own.length ? [{ id: "org", label: t.t("Your organisation"), items: own }] : []),
    ...STARTER_GROUPS.map((g) => ({ ...g, label: g.label && t.t(g.label), items: STARTER_THEMES.filter((s) => s.group === g.id) })),
  ];
  return (
    <section className="starters" aria-label={t.t("Start from")}>
      <div className="starters__head">
        <h4>{t.t("Start from")}</h4>
        {onCancel && <button type="button" className="btn btn--ghost btn--sm" onClick={onCancel}>{t.t("Cancel")}</button>}
      </div>
      {groups.map((g) => (
        <div key={g.id || "default"} className="starters__group">
          {g.label && <h5 className="starters__group-title">{g.label}</h5>}
          <div className="starters__grid">
            {g.items.map((s) => {
              const name = s.own ? s.label : t.t(s.label);
              return (
                <div key={s.id}>
                  <button type="button" className={"starters__pick" + (selected === s.id ? " is-on" : "")} aria-pressed={selected === s.id} onClick={() => onPick(s.id === "default" ? "" : name, s.look, s.id)}>
                    <span className="starters__page pv" data-theme="light" style={brandVars(s.look) as CSSProperties}>
                      <span className="starters__title">{sample?.title || t.t("Erik's stolen elephants")}</span>
                      <span className="starters__card">{sample?.text || t.t("Hi! Someone has stolen my elephants. Can you help me find them?")}</span>
                      <span className="starters__btn">{t.t("Answer")}</span>
                    </span>
                    <span className="starters__name">{name}</span>
                  </button>
                </div>
              );
            })}
          </div>
        </div>
      ))}
      {ownLookHint && <p className="field__hint">{t.t("To start from the adventure's own look, use Save this look as a theme under Adventure settings.")}</p>}
    </section>
  );
}
