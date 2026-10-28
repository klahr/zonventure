import { useEffect, useRef, useState, type CSSProperties } from "react";
import { beatMs, beepHz, warmthAt, type Band, type Warmth } from "../sensors.ts";
import type { Renderer } from "./context.ts";
import "./radar.css";

const WORD: Record<Band, string> = {
  there: "You're there!",
  burning: "Burning!",
  hot: "Hot",
  warmer: "Warmer",
  cold: "Cold",
  freezing: "Freezing",
};

/** What a designer's preview shows: warm, but not there yet. */
const PREVIEW: Warmth = { band: "warmer", heat: 0.5 };

/**
 * Whether the radar beeps, kept across radars for the page's life: a
 * player who turned it on at one wants it at the next. A browser lets a
 * page make sound only after a tap, so it starts off.
 */
let sounding = false;

/**
 * Hot and cold: a pulse that quickens and reddens as the team nears the
 * zone. It never says how far or which way; finding it is the puzzle.
 * Turned on, it also beeps and buzzes with each beat, faster and higher
 * as it warms, so a player who cannot see the screen, or keeps the phone
 * in a pocket, can play it by ear.
 */
export const RadarBlock: Renderer<"radar"> = ({ block, ctx }) => {
  const { t, position } = ctx;
  const w = !block.place ? null : ctx.preview ? PREVIEW : position ? warmthAt(position, block.place) : null;
  const [sound, setSound] = useState(sounding && !ctx.preview);
  useBeat(sound ? w : null);
  if (!block.place) return null;
  const toggle = () => {
    sounding = !sound;
    setSound(sounding);
  };
  return (
    <figure className={"pv-radar" + (w ? ` is-${w.band}` : " is-waiting")} style={{ "--heat": w?.heat ?? 0 } as CSSProperties}>
      <div className="pv-radar__scope" aria-hidden="true">
        <span className="pv-radar__ring" />
        <span className="pv-radar__ring" />
        <span className="pv-radar__dot" />
      </div>
      {/* Only the word is announced, so a screen reader hears each change of band once. */}
      <figcaption className="pv-radar__word" role="status">
        {w ? t(WORD[w.band]) : t("Finding your location…")}
      </figcaption>
      {!ctx.preview && (
        <button type="button" className="pv-btn" aria-pressed={sound} onClick={toggle}>
          {sound ? t("Stop the beeping") : t("Beep as it gets warmer")}
        </button>
      )}
    </figure>
  );
};

/**
 * Beeps and buzzes once a beat while w is set: one short tone, pitched
 * by the heat, and a short buzz where the phone can. At the zone it
 * holds a steady double beep. The beat reads the latest warmth each
 * time, so it speeds up as the team walks without starting over.
 */
function useBeat(w: Warmth | null) {
  const latest = useRef(w);
  latest.current = w;
  const on = w != null;
  useEffect(() => {
    if (!on) return;
    const Ctx = typeof AudioContext === "undefined" ? null : AudioContext;
    const audio = Ctx ? new Ctx() : null;
    const canBuzz = typeof navigator !== "undefined" && typeof navigator.vibrate === "function";
    let timer: ReturnType<typeof setTimeout>;
    const beat = () => {
      const now = latest.current;
      if (!now) return;
      const there = now.band === "there";
      if (audio) {
        beep(audio, beepHz(now.heat), 0);
        if (there) beep(audio, beepHz(now.heat), 0.18);
      }
      if (canBuzz) navigator.vibrate(there ? [60, 120, 60] : 50);
      timer = setTimeout(beat, there ? 1200 : beatMs(now.heat));
    };
    void audio?.resume().catch(() => {});
    beat();
    return () => {
      clearTimeout(timer);
      if (canBuzz) navigator.vibrate(0);
      void audio?.close().catch(() => {});
    };
  }, [on]);
}

/** A short tone at hz, `at` seconds from now, faded in and out so it does not click. */
function beep(audio: AudioContext, hz: number, at: number) {
  const start = audio.currentTime + at;
  const osc = audio.createOscillator();
  const gain = audio.createGain();
  osc.frequency.value = hz;
  gain.gain.setValueAtTime(0, start);
  gain.gain.linearRampToValueAtTime(0.3, start + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.001, start + 0.12);
  osc.connect(gain).connect(audio.destination);
  osc.start(start);
  osc.stop(start + 0.13);
}
