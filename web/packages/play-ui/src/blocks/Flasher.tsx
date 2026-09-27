import { useEffect, useMemo, useRef, useState } from "react";
import { morseFlashes, vibratePattern } from "../gadgets.ts";
import type { Renderer } from "./context.ts";
import "./gadgets.css";

/**
 * A lamp that flashes the author's message in Morse, and buzzes it too on
 * a phone that can vibrate. It never shows the message: decoding it is
 * the puzzle. Never faster than 3 Hz (see morseUnit), and a soft glow
 * rather than a white flash under prefers-reduced-motion (gadgets.css).
 */
export const FlasherBlock: Renderer<"flasher"> = ({ block, ctx }) => {
  const { t } = ctx;
  const flashes = useMemo(() => morseFlashes(block.text ?? "", block.speed), [block.text, block.speed]);
  const [playing, setPlaying] = useState(false);
  const [lit, setLit] = useState(false);
  const canBuzz = typeof navigator !== "undefined" && typeof navigator.vibrate === "function";
  const [buzz, setBuzz] = useState(canBuzz);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const stop = () => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    if (canBuzz) navigator.vibrate(0);
    setLit(false);
    setPlaying(false);
  };
  useEffect(() => stop, []);

  const play = () => {
    stop();
    if (!flashes.length) return;
    setPlaying(true);
    if (buzz && canBuzz) navigator.vibrate(vibratePattern(flashes));
    const step = (i: number) => {
      if (i >= flashes.length) {
        setLit(false);
        setPlaying(false);
        timer.current = null;
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
        <span className="pv-gadget__hint">{t("Watch the lamp: short and long flashes spell a message.")}</span>
        <div className="pv-flasher__controls">
          {!ctx.preview &&
            (playing ? (
              <button type="button" className="pv-btn" onClick={stop}>
                {t("Stop")}
              </button>
            ) : (
              <button type="button" className="pv-btn pv-btn--primary" onClick={play} disabled={!flashes.length}>
                {t("Play the signal")}
              </button>
            ))}
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
