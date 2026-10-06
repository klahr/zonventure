import { useEffect, useRef, useState } from "react";
import type { I18n } from "@zonventure/i18n";
import { CHOICES } from "./cards.ts";
import { Icon } from "./Icons.tsx";
import { TRAIL_THEMES, themeById, themeStyle, type TrailTheme } from "./themes.ts";

/** The looks to pick from, each a little card in its own colours and font. */
export function ThemePicker({ i18n, selected, onPick, busy }: { i18n: I18n; selected: string; onPick: (t: TrailTheme) => void; busy?: boolean }) {
  const t = i18n.t;
  const strip = useRef<HTMLDivElement>(null);
  // The chosen look stays in view. The strip scrolls, never the page:
  // in a frame on the landing page, scrollIntoView would move it too.
  useEffect(() => {
    const el = strip.current?.querySelector<HTMLElement>(".trail-theme.is-on");
    const box = strip.current;
    if (!el || !box) return;
    const left = el.offsetLeft - box.offsetLeft;
    if (left < box.scrollLeft || left + el.offsetWidth > box.scrollLeft + box.clientWidth) {
      box.scrollTo({ left: Math.max(0, left - 12), behavior: "smooth" });
    }
  }, [selected]);
  return (
    <div className="trail-themes" ref={strip} role="radiogroup" aria-label={t("Look")}>
      {TRAIL_THEMES.map((th) => (
        <button
          key={th.id}
          type="button"
          role="radio"
          aria-checked={th.id === selected}
          className={"trail-theme" + (th.id === selected ? " is-on" : "")}
          disabled={busy}
          style={themeStyle(th.look)}
          onClick={() => onPick(th)}
        >
          <span className="trail-theme__swatch">
            <span className="trail-theme__band" />
            <span className="trail-theme__dot" />
          </span>
          <span className="trail-theme__name">{t(th.label)}</span>
          {th.id === selected && (
            <span className="trail-theme__check" aria-hidden="true">
              <Icon name="check" size={14} />
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

/** A card as the creator draws it, not to edit: for showing a look. */
export function PaperPreview({ i18n, text, options, correct, number }: { i18n: I18n; text: string; options: string[]; correct: string; number: number }) {
  const t = i18n.t;
  return (
    <div className="trail-paper trail-paper--quiz trail-paper--preview" aria-hidden="true">
      <div className="trail-paper__head">
        <span className="trail-paper__badge">{number}</span>
        <span className="trail-paper__no">
          <small>{t("Card {{n}}", { n: number })}</small>
          {t("Question with options")}
        </span>
      </div>
      <p className="trail-paper__text trail-paper__text--static">{text}</p>
      <div className="trail-options">
        {CHOICES.map((mark, i) => (
          <div className={"trail-option" + (mark === correct ? " is-right" : "")} key={mark}>
            <span className={"trail-mark" + (mark === correct ? " is-right" : "")}>{mark === correct ? <Icon name="check" size={18} /> : mark}</span>
            <span className="trail-option__text">{options[i]}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

const SHOW_MS = 2600;

/**
 * A sample card that tries the looks on, one after another, until the
 * visitor does anything on the page — then the look on show, or the one
 * they pick below it, is theirs. onChange reports it either way.
 */
export function ThemeShowcase({ i18n, value, onChange, sample }: { i18n: I18n; value: string; onChange: (id: string) => void; sample: { text: string; options: string[]; correct: string } }) {
  const t = i18n.t;
  const [still, setStill] = useState(false);
  const latest = useRef(value);
  latest.current = value;

  useEffect(() => {
    if (still) return;
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    if (reduce) return setStill(true);
    const stop = () => setStill(true);
    const events = ["pointerdown", "keydown", "wheel", "touchstart"] as const;
    for (const e of events) window.addEventListener(e, stop, { once: true, passive: true });
    const id = setInterval(() => {
      if (document.hidden) return;
      const i = TRAIL_THEMES.findIndex((x) => x.id === latest.current);
      onChange(TRAIL_THEMES[(i + 1) % TRAIL_THEMES.length]!.id);
    }, SHOW_MS);
    return () => {
      clearInterval(id);
      for (const e of events) window.removeEventListener(e, stop);
    };
  }, [still, onChange]);

  const theme = themeById(value) ?? TRAIL_THEMES[0]!;
  return (
    <section className="trail-showcase" style={themeStyle(theme.look)} aria-label={t("Look")}>
      <div className="trail-showcase__stage">
        <div className="trail-showcase__card" key={theme.id}>
          <PaperPreview i18n={i18n} number={1} {...sample} />
        </div>
        <span className="trail-showcase__label">
          <Icon name="palette" size={14} />
          {t(theme.label)}
        </span>
      </div>
      <p className="trail-muted">{still ? t("Pick a look. You can change it any time.") : t("Trying on looks… tap one to keep it.")}</p>
      <ThemePicker i18n={i18n} selected={theme.id} onPick={(th) => onChange(th.id)} />
    </section>
  );
}
