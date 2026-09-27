import { useEffect, useRef, useState } from "react";
import { play } from "@zonventure/api";
import type { Renderer } from "./context.ts";
import "./gadgets.css";

type Phase = "idle" | "loading" | "playing" | "failed";

/**
 * A sound with tricks: played backwards, where the author hid a reversed
 * message, and drawn as a scrolling spectrogram, where a picture may hide
 * in its frequencies. The sound is decoded with Web Audio, its context
 * made inside the tap: iOS keeps one made later suspended.
 */
export const AudioFxBlock: Renderer<"audio"> = ({ block, ctx }) => {
  const { t } = ctx;
  const [phase, setPhase] = useState<Phase>("idle");
  const [backwards, setBackwards] = useState(false);
  const canvas = useRef<HTMLCanvasElement>(null);
  const audio = useRef<{ ctx: AudioContext; buffer: AudioBuffer; reversed: AudioBuffer | null } | null>(null);
  const playing = useRef<{ source: AudioBufferSourceNode; frame: number } | null>(null);

  const stop = () => {
    const p = playing.current;
    if (!p) return;
    playing.current = null;
    cancelAnimationFrame(p.frame);
    p.source.onended = null;
    try {
      p.source.stop();
    } catch {
      // Already stopped.
    }
  };
  useEffect(
    () => () => {
      stop();
      void audio.current?.ctx.close().catch(() => {});
    },
    [],
  );

  const start = async () => {
    if (phase === "playing") {
      stop();
      setPhase("idle");
      return;
    }
    if (typeof AudioContext === "undefined") return setPhase("failed");
    let a = audio.current;
    if (!a) {
      const ac = new AudioContext();
      setPhase("loading");
      try {
        const bytes = await (await fetch(play.mediaURL(block.media))).arrayBuffer();
        a = audio.current = { ctx: ac, buffer: await ac.decodeAudioData(bytes), reversed: null };
      } catch {
        void ac.close().catch(() => {});
        return setPhase("failed");
      }
    }
    void a.ctx.resume();
    let buffer = a.buffer;
    if (backwards) {
      a.reversed ??= reverse(a.ctx, a.buffer);
      buffer = a.reversed;
    }
    const source = a.ctx.createBufferSource();
    source.buffer = buffer;
    const analyser = a.ctx.createAnalyser();
    analyser.fftSize = 1024;
    source.connect(analyser).connect(a.ctx.destination);
    source.onended = () => {
      stop();
      setPhase("idle");
    };
    source.start();
    playing.current = { source, frame: 0 };
    setPhase("playing");
    if (block.spectrogram) draw(analyser);
  };

  // A waterfall: each frame shifts the picture left a pixel and paints the
  // newest spectrum down the right edge, low notes at the bottom.
  const draw = (analyser: AnalyserNode) => {
    const c = canvas.current;
    const g = c?.getContext("2d");
    if (!c || !g) return;
    const bins = new Uint8Array(analyser.frequencyBinCount);
    const tick = () => {
      if (!playing.current) return;
      analyser.getByteFrequencyData(bins);
      g.drawImage(c, -1, 0);
      const h = c.height;
      for (let y = 0; y < h; y++) {
        const v = bins[Math.floor(((h - 1 - y) / h) * bins.length)]! / 255;
        g.fillStyle = `hsl(${260 - v * 220} 90% ${v * 60}%)`;
        g.fillRect(c.width - 1, y, 1, 1);
      }
      playing.current.frame = requestAnimationFrame(tick);
    };
    playing.current!.frame = requestAnimationFrame(tick);
  };

  return (
    <figure className="pv-audiofx">
      {block.spectrogram && <canvas ref={canvas} className="pv-audiofx__spectrum" width={320} height={128} aria-label={t("The sound's spectrum")} />}
      <div className="pv-audiofx__controls">
        <button type="button" className="pv-btn pv-btn--primary" onClick={() => void start()} disabled={ctx.preview || phase === "loading"}>
          {phase === "playing" ? t("Stop") : phase === "loading" ? t("Loading…") : t("Play")}
        </button>
        {block.reverse && (
          <label className="pv-audiofx__toggle">
            <input type="checkbox" checked={backwards} disabled={phase === "playing"} onChange={(e) => setBackwards(e.target.checked)} />
            {t("Play backwards")}
          </label>
        )}
      </div>
      {block.alt && <figcaption className="pv-muted">{block.alt}</figcaption>}
      {phase === "failed" && <p className="pv-gadget__note">{t("This sound can't be played here.")}</p>}
    </figure>
  );
};

function reverse(ac: AudioContext, b: AudioBuffer): AudioBuffer {
  const out = ac.createBuffer(b.numberOfChannels, b.length, b.sampleRate);
  for (let ch = 0; ch < b.numberOfChannels; ch++) {
    const src = b.getChannelData(ch);
    const dst = out.getChannelData(ch);
    for (let i = 0, n = src.length; i < n; i++) dst[i] = src[n - 1 - i]!;
  }
  return out;
}
