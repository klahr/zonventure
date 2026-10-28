import { useEffect, useMemo, useRef, useState } from "react";
import { morseFlashes, vibratePattern, type Flash } from "../gadgets.ts";
import type { Renderer } from "./context.ts";
import "./gadgets.css";

/**
 * A lamp that flashes the author's message in Morse, beeps it, and buzzes
 * it too on a phone that can vibrate: an iPhone cannot, so the beep is
 * what a player who cannot see the lamp goes by. It never shows the
 * message: decoding it is the puzzle. Never faster than 3 Hz (see morseUnit), and a soft glow
 * rather than a white flash under prefers-reduced-motion (gadgets.css).
 */
export const FlasherBlock: Renderer<"flasher"> = ({ block, ctx }) => {
  const { t } = ctx;
  const flashes = useMemo(() => morseFlashes(block.text ?? "", block.speed), [block.text, block.speed]);
  const [playing, setPlaying] = useState(false);
  const [lit, setLit] = useState(false);
  const canBuzz = typeof navigator !== "undefined" && typeof navigator.vibrate === "function";
  const [buzz, setBuzz] = useState(canBuzz);
  const canBeep = typeof AudioContext !== "undefined";
  const [sound, setSound] = useState(canBeep);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const audio = useRef<AudioContext | null>(null);

  const stop = () => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    if (canBuzz) navigator.vibrate(0);
    void audio.current?.close().catch(() => {});
    audio.current = null;
    setLit(false);
    setPlaying(false);
  };
  useEffect(() => stop, []);

  const play = () => {
    stop();
    if (!flashes.length) return;
    setPlaying(true);
    if (buzz && canBuzz && !ctx.preview) navigator.vibrate(vibratePattern(flashes));
    // Pressing play is the tap a browser wants before a page makes sound.
    if (sound && canBeep) audio.current = beepFlashes(flashes);
    const step = (i: number) => {
      if (i >= flashes.length) {
        setLit(false);
        setPlaying(false);
        timer.current = null;
        void audio.current?.close().catch(() => {});
        audio.current = null;
        return;
      }
      setLit(flashes[i]!.on);
      timer.current = setTimeout(() => step(i + 1), flashes[i]!.ms);
    };
    step(0);
  };

  return (
    <figure className="pv-flasher">
      <div className={"pv-flasher__lamp" + (lit ? " is-lit" : "")} role="img" aria-label={playing ? t("The lamp is flashing") : t("A signal lamp")} />
      <figcaption className="pv-gadget__caption">
        <span className="pv-gadget__hint">{t("Watch the lamp or listen: short and long signals spell a message.")}</span>
        <div className="pv-flasher__controls">
          {/* The lamp needs no sensor or permission, so a preview plays it too; only the buzz is the phone's. */}
          {playing ? (
            <button type="button" className="pv-btn" onClick={stop}>
              {t("Stop")}
            </button>
          ) : (
            <button type="button" className="pv-btn pv-btn--primary" onClick={play} disabled={!flashes.length}>
              {t("Play the signal")}
            </button>
          )}
          {canBeep && (
            <label className="pv-flasher__buzz">
              <input type="checkbox" checked={sound} disabled={playing} onChange={(e) => setSound(e.target.checked)} />
              {t("Beep too")}
            </label>
          )}
          {canBuzz && !ctx.preview && (
            <label className="pv-flasher__buzz">
              <input type="checkbox" checked={buzz} disabled={playing} onChange={(e) => setBuzz(e.target.checked)} />
              {t("Vibrate too")}
            </label>
          )}
        </div>
      </figcaption>
    </figure>
  );
};

/**
 * Plays the flashes as a tone, scheduled on the audio clock so a dot and
 * a dash keep their lengths however busy the page is: on while the lamp
 * is lit, silent while it is dark, with a few ms of fade so it does not
 * click. Closing the context it returns stops it.
 */
function beepFlashes(flashes: Flash[]): AudioContext {
  const audio = new AudioContext();
  const osc = audio.createOscillator();
  const gain = audio.createGain();
  osc.frequency.value = 700;
  gain.gain.value = 0;
  osc.connect(gain).connect(audio.destination);
  let at = audio.currentTime + 0.05;
  for (const f of flashes) {
    const ms = f.ms / 1000;
    if (f.on) {
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(0.3, at + 0.005);
      gain.gain.setValueAtTime(0.3, at + ms - 0.005);
      gain.gain.linearRampToValueAtTime(0, at + ms);
    }
    at += ms;
  }
  osc.start();
  osc.stop(at + 0.05);
  return audio;
}
