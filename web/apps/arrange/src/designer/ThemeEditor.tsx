import type { CSSProperties, ReactNode } from "react";
import type { play } from "@zonventure/api";
import { brandVars, loadFont } from "@zonventure/play-ui";
import { Plus } from "lucide-react";
import { useT } from "../i18n.tsx";
import { LookControls } from "./Look.tsx";
import { ICON } from "./dz/ui.tsx";

export interface StripTheme {
  key: string;
  name: string;
  look: play.Brand;
}

/**
 * The themes to edit, side by side: each a small card in its own page
 * colour, background, title face and accent, and New theme last.
 */
export function ThemeStrip({ themes, current, adding, onPick, onAdd }: { themes: StripTheme[]; current: string | null; adding: boolean; onPick: (key: string) => void; onAdd: () => void }) {
  const t = useT();
  return (
    <nav className="theme-strip" aria-label={t.t("Themes")}>
      {themes.map((th) => {
        loadFont(th.look.heading_font ?? th.look.font);
        const on = !adding && th.key === current;
        return (
          <button key={th.key} type="button" className={"theme-strip__item" + (on ? " is-on" : "")} aria-current={on} onClick={() => onPick(th.key)}>
            <span className="theme-strip__thumb look__thumb" style={brandVars(th.look) as CSSProperties} aria-hidden>
              <span className="theme-strip__aa">Aa</span>
              <span className="theme-strip__dot" />
            </span>
            <span className="theme-strip__name">{th.name || t.t("Untitled theme")}</span>
          </button>
        );
      })}
      <button type="button" className={"theme-strip__item theme-strip__item--add" + (adding ? " is-on" : "")} aria-pressed={adding} onClick={onAdd}>
        <span className="theme-strip__thumb look__thumb--add" aria-hidden>
          <Plus size={ICON.md} />
        </span>
        <span className="theme-strip__name">{t.t("New theme")}</span>
      </button>
    </nav>
  );
}

/**
 * One theme: its name, what can be done with it, a word on where it is
 * used, and the look's controls beside a phone that shows every change.
 */
export function ThemeEditor({ name, onName, look, onLook, media, actions, note, preview }: {
  name: string;
  onName: (name: string) => void;
  look: play.Brand;
  onLook: (look: play.Brand) => void;
  /** The look may use pictures and sounds of its own. */
  media: boolean;
  actions: ReactNode;
  note: ReactNode;
  preview: ReactNode;
}) {
  const t = useT();
  return (
    <div className="theme-ed">
      <div className="theme-ed__form">
        <div className="theme-ed__head">
          <label className="field theme-ed__name">
            <span className="field__label">{t.t("Name")}</span>
            <input className="field__input" value={name} maxLength={80} onChange={(e) => onName(e.target.value)} />
          </label>
          <div className="look__actions">{actions}</div>
        </div>
        {note}
        <LookControls brand={look} onChange={onLook} media={media} />
      </div>
      <aside className="theme-ed__preview">{preview}</aside>
    </div>
  );
}
