import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Markdown } from "../Markdown.tsx";
import { HOLD_START, holdStep, loudness, rms, type Hold } from "../sensors.ts";
import { fillTeam, type Renderer } from "./context.ts";
import "./noise.css";

/** How long it must stay loud: a shout, not a cough. */
const HOLD_MS = 500;

type Phase = "idle" | "asking" | "on" | "done" | "denied" | "none";

/**
 * A loudness meter on the phone's microphone: staying over the author's
 * threshold for half a second reveals the text. Nothing is recorded or
 * sent; the sound is only measured, here.
 */
export const NoiseBlock: Renderer<"noise"> = ({ block, ctx }) => {
  const { t } = ctx;
  const threshold = block.threshold ?? 70;
  const [phase, setPhase] = useState<Phase>("idle");
  const [level, setLevel] = useState(0);
  const stop = useRef<(() => void) | null>(null);

  useEffect(() => () => stop.current?.(), []);

  // A simulated room needs no microphone; its loudness is checked on a
  // timer, as the level checks a simulated pose.
  const simulated = ctx.motion?.loudness;
  const simRef = useRef(simulated);
  simRef.current = simulated;
  useEffect(() => {
    if (simulated === undefined || phase === "done") return;
    let hold: Hold = HOLD_START;
    const id = setInterval(() => {
      const now = simRef.current ?? 0;
      hold = holdStep(hold, now >= threshold, performance.now(), HOLD_MS);
      if (hold.done) setPhase("done");
    }, 100);
    return () => clearInterval(id);
  }, [simulated !== undefined, phase, threshold]);

  const start = () => {
    if (!navigator.mediaDevices?.getUserMedia || typeof AudioContext === "undefined") return setPhase("none");
    setPhase("asking");
    // Made inside the tap: iOS keeps a context made later suspended.
    const audio = new AudioContext();
    let frame = 0;
    let stream: MediaStream | null = null;
    let live = true;
    stop.current = () => {
      live = false;
      cancelAnimationFrame(frame);
      stream?.getTracks().forEach((tr) => tr.stop());
      void audio.close().catch(() => {});
      stop.current = null;
    };
    // The browser's voice processing would level a shout down to talk.
    navigator.mediaDevices
      .getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } })
      .then((s) => {
        // Unmounted while the browser asked: let go of the microphone at once.
        if (!live) return s.getTracks().forEach((tr) => tr.stop());
        stream = s;
        void audio.resume();
        const analyser = audio.createAnalyser();
        analyser.fftSize = 2048;
        audio.createMediaStreamSource(s).connect(analyser);
        const samples = new Float32Array(analyser.fftSize);
        let hold: Hold = HOLD_START;
        let shown = 0;
        setPhase("on");
        const tick = () => {
          analyser.getFloatTimeDomainData(samples);
          const now = loudness(rms(samples));
          hold = holdStep(hold, now >= threshold, performance.now(), HOLD_MS);
          if (hold.done) {
            stop.current?.();
            setLevel(now);
            setPhase("done");
            return;
          }
          // The needle falls back slowly, as a meter's does, so a word stays readable.
          shown = Math.max(now, shown - 1.5);
          setLevel(Math.round(shown));
          frame = requestAnimationFrame(tick);
        };
        frame = requestAnimationFrame(tick);
      })
      .catch((e: unknown) => {
        stop.current?.();
        setPhase(e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "SecurityError") ? "denied" : "none");
      });
  };

  // The designer's preview: talking, not yet loud enough.
  const shown = simulated ?? (ctx.preview ? Math.max(5, threshold - 20) : level);
  const done = phase === "done";

  return (
    <figure className={"pv-noise" + (done ? " is-done" : "")}>
      <div
        className={"pv-noise__meter" + (shown >= threshold ? " is-over" : "")}
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(shown)}
        aria-label={t("Loudness")}
        style={{ "--level": `${shown}%`, "--threshold": `${threshold}%` } as CSSProperties}
      >
        <span className="pv-noise__fill" />
        <span className="pv-noise__mark" title={t("Loud enough")} />
      </div>
      <figcaption className="pv-noise__caption">
        {done ? (
          <span className="pv-noise__cheer">{t("Loud and clear!")}</span>
        ) : phase === "denied" ? (
          <span className="pv-noise__note">{t("Microphone access was turned down. Allow it in the browser's settings and try again.")}</span>
        ) : phase === "none" ? (
          <span className="pv-noise__note">{t("This phone's microphone can't be used here.")}</span>
        ) : phase === "on" || simulated !== undefined ? (
          <span className="pv-noise__hint">{shown >= threshold ? t("Keep it up!") : t("Louder!")}</span>
        ) : (
          <span className="pv-noise__hint">{t("Make some noise, past the line.")}</span>
        )}
        {!ctx.preview && (phase === "idle" || phase === "denied") && (
          <button type="button" className="pv-btn pv-btn--primary" onClick={start}>
            {t("Start listening")}
          </button>
        )}
      </figcaption>
      {done && block.text && <Markdown className="pv-noise__text" text={fillTeam(block.text, ctx)} />}
    </figure>
  );
};
