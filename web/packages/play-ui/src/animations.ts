import type { play } from "@zonventure/api";

/**
 * A block's animation as a CSS `animation` value — an entrance as it
 * appears, then an attention effect — from the player's own keyframes
 * (animations.css), named as animate.style names its effects so authors
 * who know them recognise them. Only names from these lists ever reach a
 * style; anything else is dropped. Under reduced motion play-ui.css
 * switches every animation off, so the block simply shows.
 */
export const ENTRANCES = ["fadeIn", "fadeInUp", "fadeInDown", "slideInLeft", "slideInRight", "zoomIn", "bounceIn", "flipInX", "backInUp", "lightSpeedInRight", "rollIn"] as const;
export const ATTENTIONS = ["pulse", "bounce", "shakeX", "wobble", "tada", "heartBeat", "swing", "rubberBand", "jello", "flash", "headShake"] as const;

const ENTER = new Set<string>(ENTRANCES);
const ATTEND = new Set<string>(ATTENTIONS);

/** The step's own gentle rise (play-ui.css), for a block that only draws the eye. */
const RISE = "pv-enter 0.7s cubic-bezier(0.2, 0.8, 0.2, 1)";

export function animationCSS(a: play.BlockAnimation | undefined): string | undefined {
  if (!a) return undefined;
  const entrance = a.entrance && ENTER.has(a.entrance) ? a.entrance : undefined;
  const attention = a.attention && ATTEND.has(a.attention) ? a.attention : undefined;
  const delay = Math.min(10, Math.max(0, Number.isFinite(a.delay) ? a.delay! : 0));
  if (!entrance && !attention && !delay) return undefined;
  const dur = a.duration ? Math.min(5, Math.max(0.2, a.duration)) : 1;
  const s = (n: number) => `${Math.round(n * 100) / 100}s`;
  const enterFor = entrance ? dur : 0.7;
  const parts = [entrance ? `pv-a-${entrance} ${s(dur)} ease-out ${s(delay)} both` : `${RISE} ${s(delay)} both`];
  if (attention) {
    const count = a.repeat === "forever" ? "infinite" : a.repeat === "three" ? "3" : "1";
    // No backwards fill: its first frame would hold the block still while it enters.
    parts.push(`pv-a-${attention} ${s(dur)} ease-in-out ${s(delay + enterFor)} ${count} forwards`);
  }
  return parts.join(", ");
}

/** The ways a step may enter (look.Transitions). */
export const TRANSITIONS = ["none", "fade", "slide", "zoom", "flip", "pop"] as const;
