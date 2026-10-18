import { useEffect, useState, type CSSProperties } from "react";
import { adventures, type play } from "@zonventure/api";
import type { ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, THEME_COVERS, brandVars, loadFont } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { STARTER_GROUPS, STARTER_THEMES } from "@zonventure/play-ui";
import "./starters.css";
import { Button } from "./dz/ui.tsx";

/** A look as JSON with its keys in order, so two can be compared however they were built. */
export function canon(v: unknown): string {
  return JSON.stringify(v, (_, x: unknown) =>
    x && typeof x === "object" && !Array.isArray(x) ? Object.fromEntries(Object.entries(x as Record<string, unknown>).sort(([p], [q]) => (p < q ? -1 : 1))) : x,
  );
}

/**
 * A look's cover picture, as the players see it: its own, else that of
 * the starter theme it is (a theme copied from a starter and unchanged).
 */
function coverFor(id: string, look: ThemeLook): string | undefined {
  const own = (look.style as play.LookStyle | undefined)?.cover;
  if (own) return `/media/${own}`;
  const starter = THEME_COVERS[id] ? id : STARTER_THEMES.find((s) => canon(s.look) === canon(look))?.id;
  return starter ? THEME_COVERS[starter]?.small : undefined;
}

/**
 * Looks to choose from, large enough to choose by eye: each look on a
 * small page with a title, a text card and an answer button, in its own
 * colours, fonts, background and cards. The organisation's come first,
 * after the default; chips
 * narrow the gallery to one group.
 */
export function StarterGallery({ onPick, onCancel, selected, sample, heading }: {
  onPick: (name: string, look: ThemeLook, id: string) => void;
  onCancel?: () => void;
  /** The starter picked so far, marked. */
  selected?: string | undefined;
  /** The title and text on each sample page; the adventure's own when known. */
  sample?: { title: string; text: string };
  /** Above the gallery; "Start from" when not given, none when empty. */
  heading?: string;
}) {
  const t = useT();
  const [library, setLibrary] = useState<adventures.OrgTheme[]>([]);
  const [only, setOnly] = useState<string | null>(null);
  const starters = [{ id: "default", label: "Default theme", group: "", look: DEFAULT_LOOK as ThemeLook }, ...STARTER_THEMES];
  const own = library.map((th) => ({ id: `org-${th.id}`, label: th.name, raw: true, look: th.look as ThemeLook }));
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
  const groups: { id: string; label: string; chip?: string; items: { id: string; label: string; look: ThemeLook; raw?: boolean }[] }[] = [
    { id: "", label: "", items: [starters[0]!] },
    ...(own.length ? [{ id: "org", label: t.t("Your organisation"), items: own }] : []),
    ...STARTER_GROUPS.map((g) => ({ id: g.id, label: g.label && t.t(g.label), chip: g.label ? t.t(g.label) : t.t("More themes"), items: STARTER_THEMES.filter((s) => s.group === g.id) })),
  ];
  const head = heading ?? t.t("Start from");
  return (
    <section className="starters" aria-label={head || t.t("Themes")}>
      {(head || onCancel) && (
        <div className="starters__head">
          {head && <h4>{head}</h4>}
          {onCancel && <Button size="sm" onClick={onCancel}>{t.t("Cancel")}</Button>}
        </div>
      )}
      <div className="starters__chips" role="group" aria-label={t.t("Show")}>
        {[{ id: null, chip: t.t("All") }, ...groups.filter((g) => g.id).map((g) => ({ id: g.id, chip: g.chip ?? g.label }))].map((c) => (
          <button key={c.id ?? "all"} type="button" className={"starters__chip" + (only === c.id ? " is-on" : "")} aria-pressed={only === c.id} onClick={() => setOnly(c.id)}>
            {c.chip}
          </button>
        ))}
      </div>
      {groups.filter((g) => only === null || g.id === only).map((g) => (
        <div key={g.id || "default"} className="starters__group">
          {g.label && <h5 className="starters__group-title">{g.label}</h5>}
          <div className="starters__grid">
            {g.items.map((s) => {
              const name = s.raw ? s.label : t.t(s.label);
              const cover = s.id === "default" ? undefined : coverFor(s.id, s.look);
              return (
                <div key={s.id}>
                  <button type="button" className={"starters__pick" + (selected === s.id ? " is-on" : "")} aria-pressed={selected === s.id} onClick={() => onPick(s.id === "default" ? "" : name, s.look, s.id)}>
                    <span className="starters__page pv" data-theme="light" style={brandVars(s.look) as CSSProperties}>
                      {cover && <span className="starters__cover" style={{ backgroundImage: `url(${cover})` }} />}
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
    </section>
  );
}
