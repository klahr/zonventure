import { useState } from "react";
import { ATTENTIONS, ENTRANCES, type Animation, type Block } from "@zonventure/graph";
import { animationCSS } from "@zonventure/play-ui";
import { Play } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import type { T } from "./types.ts";

/** Asks the preview to play the step's animations again; the simulator listens. */
export const REPLAY_EVENT = "zv:replay-animations";
export const replayAnimations = () => window.dispatchEvent(new Event(REPLAY_EVENT));

/**
 * The animation on a sample card, right beside its controls: it plays on
 * every change, without waiting for the preview to place the team again.
 * The delay is left out, so the effect shows at once.
 */
function Demo({ animation, replay, label }: { animation: Animation; replay: number; label: string }) {
  const css = animationCSS({ ...animation, delay: 0 });
  return (
    <div className="blk__anim-demo" aria-hidden>
      <span key={replay} className="blk__anim-card" style={css ? { animation: css } : undefined}>
        {label}
      </span>
    </div>
  );
}

/** The effects in the author's words; the names are animate.style's, kept for those who know them. */
const ENTRANCE_LABELS: Record<(typeof ENTRANCES)[number], string> = {
  fadeIn: "Fade in",
  fadeInUp: "Fade in from below",
  fadeInDown: "Fade in from above",
  slideInLeft: "Slide in from the left",
  slideInRight: "Slide in from the right",
  zoomIn: "Zoom in",
  bounceIn: "Bounce in",
  flipInX: "Flip in",
  backInUp: "Rise up",
  lightSpeedInRight: "Whoosh in",
  rollIn: "Roll in",
};
const ATTENTION_LABELS: Record<(typeof ATTENTIONS)[number], string> = {
  pulse: "Pulse",
  bounce: "Bounce",
  shakeX: "Shake",
  wobble: "Wobble",
  tada: "Tada!",
  heartBeat: "Heartbeat",
  swing: "Swing",
  rubberBand: "Rubber band",
  jello: "Jelly",
  flash: "Flash",
  headShake: "Head shake",
};

/**
 * How a block moves: an entrance as it appears and an effect to catch the
 * eye after, with a delay so blocks can arrive one after another. The
 * preview plays each change; Play again replays the step.
 */
export function AnimationEditor({ block, set, t }: { block: Block; set: (b: Block) => void; t: T }) {
  const a = block.animation ?? {};
  // Bumped to play the sample again; every edit does too.
  const [demo, setDemo] = useState(0);
  const edit = (patch: { [K in keyof Animation]?: Animation[K] | undefined }) => {
    const next: Record<string, unknown> = { ...a, ...patch };
    for (const k of Object.keys(next)) if (next[k] === undefined || next[k] === "" || next[k] === 0) delete next[k];
    const { animation: _, ...rest } = block;
    set(Object.keys(next).length ? { ...rest, animation: next as Animation } : (rest as Block));
    setDemo((n) => n + 1);
    setTimeout(replayAnimations, 350);
  };
  return (
    <div className="blk__show blk__anim">
      <div className="blk__anim-grid">
        <label className="field">
          <span className="field__label">{t.t("Entrance")}</span>
          <select className="field__input" value={a.entrance ?? ""} onChange={(e) => edit({ entrance: (e.target.value || undefined) as Animation["entrance"] })}>
            <option value="">{t.t("Just appear")}</option>
            {ENTRANCES.map((n) => (
              <option key={n} value={n}>{t.t(ENTRANCE_LABELS[n])}</option>
            ))}
          </select>
        </label>
        <label className="field">
          <span className="field__label">{t.t("Then, to catch the eye")}</span>
          <select className="field__input" value={a.attention ?? ""} onChange={(e) => edit({ attention: (e.target.value || undefined) as Animation["attention"], ...(e.target.value ? {} : { repeat: undefined }) })}>
            <option value="">{t.t("Nothing")}</option>
            {ATTENTIONS.map((n) => (
              <option key={n} value={n}>{t.t(ATTENTION_LABELS[n])}</option>
            ))}
          </select>
        </label>
        {a.attention && (
          <label className="field">
            <span className="field__label">{t.t("How often")}</span>
            <select className="field__input" value={a.repeat ?? ""} onChange={(e) => edit({ repeat: (e.target.value || undefined) as Animation["repeat"] })}>
              <option value="">{t.t("Once")}</option>
              <option value="three">{t.t("Three times")}</option>
              <option value="forever">{t.t("Over and over")}</option>
            </select>
          </label>
        )}
      </div>
      <Demo animation={a} replay={demo} label={t.t("Sample")} />
      <label className="field look__slider">
        <span className="field__label">
          {t.t("Duration")} <span className="look__value">{(a.duration ?? 1).toLocaleString()} s</span>
        </span>
        <input type="range" min={0.2} max={5} step={0.1} value={a.duration ?? 1} onChange={(e) => edit({ duration: Number(e.target.value) === 1 ? undefined : Number(e.target.value) })} />
      </label>
      <label className="field look__slider">
        <span className="field__label">
          {t.t("Wait before it appears")} <span className="look__value">{(a.delay ?? 0).toLocaleString()} s</span>
        </span>
        <input type="range" min={0} max={10} step={0.25} value={a.delay ?? 0} onChange={(e) => edit({ delay: Number(e.target.value) })} />
      </label>
      <div className="blk__anim-foot">
        <button
          type="button"
          className="btn btn--ghost btn--sm"
          onClick={() => {
            setDemo((n) => n + 1);
            replayAnimations();
          }}
        >
          <Play size={ICON.sm} aria-hidden />
          {t.t("Play again")}
        </button>
        <span className="field__hint">{t.t("Phones set to reduce motion show the block without moving it.")}</span>
      </div>
    </div>
  );
}
