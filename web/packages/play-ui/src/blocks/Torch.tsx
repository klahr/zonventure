import { useEffect, useId, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { play } from "@zonventure/api";
import { fractionAt, nudge, type Fraction } from "../pictures.ts";
import type { Renderer } from "./context.ts";
import "./torch.css";

/**
 * A hidden picture, found one of four ways (block.style): a torch in the
 * dark (none), a UV lamp over invisible ink, a scratch card rubbed off,
 * or a red lens over red noise.
 */
export const TorchBlock: Renderer<"torch"> = (p) => {
  switch (p.block.style) {
    case "scratch":
      return <Scratch {...p} />;
    case "red":
      return <RedLens {...p} />;
    default:
      return <Lamp {...p} />;
  }
};

/**
 * A dark picture, explored with a torch that follows the finger and stays
 * where it was left, or with a UV lamp that makes it glow. The light is a
 * CSS mask on a second copy of the picture; it moves by custom
 * properties, not React renders, so dragging stays smooth on a phone.
 */
const Lamp: Renderer<"torch"> = ({ block, ctx }) => {
  const uv = block.style === "uv";
  const stage = useRef<HTMLDivElement>(null);
  const at = useRef<Fraction>({ x: 0.5, y: 0.5 });
  const hint = useId();
  const src = play.mediaURL(block.media);

  const shine = (p: Fraction) => {
    at.current = p;
    stage.current?.style.setProperty("--x", `${p.x * 100}%`);
    stage.current?.style.setProperty("--y", `${p.y * 100}%`);
  };
  const follow = (e: PointerEvent<HTMLDivElement>) => shine(fractionAt(e.clientX, e.clientY, e.currentTarget.getBoundingClientRect()));
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    const p = nudge(at.current, e.key);
    if (!p) return;
    e.preventDefault();
    shine(p);
  };

  return (
    <figure className={"pv-torch" + (uv ? " pv-torch--uv" : "")}>
      <div
        ref={stage}
        className="pv-torch__stage"
        role="img"
        aria-label={block.alt || ctx.t("A dark picture")}
        aria-describedby={hint}
        tabIndex={0}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          follow(e);
        }}
        onPointerMove={follow}
        onKeyDown={key}
      >
        <img className="pv-torch__dark" src={src} alt="" draggable={false} />
        <img className="pv-torch__lit" src={src} alt="" draggable={false} />
      </div>
      <figcaption id={hint} className="pv-muted">
        {uv ? ctx.t("Drag your finger to shine the UV lamp.") : ctx.t("Drag your finger to shine the torch.")}
      </figcaption>
    </figure>
  );
};

/** How wide a scratch is, as a share of the card's width. */
const SCRATCH = 0.07;

/**
 * A scratch card: a silver coat over the picture that the finger rubs
 * away where it goes. What is rubbed off stays off.
 */
const Scratch: Renderer<"torch"> = ({ block, ctx }) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  const last = useRef<{ x: number; y: number } | null>(null);
  const hint = useId();
  const label = ctx.t("Scratch here");

  // Coat the card at the size it is drawn, sharp on a dense screen.
  const coat = () => {
    const c = canvas.current;
    const g = c?.getContext("2d");
    if (!c || !g) return;
    const dpr = window.devicePixelRatio || 1;
    c.width = Math.max(1, Math.round(c.clientWidth * dpr));
    c.height = Math.max(1, Math.round(c.clientHeight * dpr));
    const grad = g.createLinearGradient(0, 0, c.width, c.height);
    grad.addColorStop(0, "#b9bcc2");
    grad.addColorStop(0.5, "#e4e6ea");
    grad.addColorStop(1, "#9ea2a9");
    g.fillStyle = grad;
    g.fillRect(0, 0, c.width, c.height);
    g.fillStyle = "rgb(0 0 0 / 0.35)";
    g.font = `700 ${Math.round(c.width / 14)}px system-ui, sans-serif`;
    g.textAlign = "center";
    g.textBaseline = "middle";
    g.fillText(label, c.width / 2, c.height / 2);
  };

  const rub = (e: PointerEvent<HTMLCanvasElement>) => {
    const c = e.currentTarget;
    const g = c.getContext("2d");
    if (!g) return;
    const box = c.getBoundingClientRect();
    const x = ((e.clientX - box.left) / box.width) * c.width;
    const y = ((e.clientY - box.top) / box.height) * c.height;
    const from = last.current ?? { x, y };
    g.globalCompositeOperation = "destination-out";
    g.lineWidth = c.width * SCRATCH;
    g.lineCap = "round";
    g.beginPath();
    g.moveTo(from.x, from.y);
    g.lineTo(x + 0.01, y);
    g.stroke();
    g.globalCompositeOperation = "source-over";
    last.current = { x, y };
  };

  return (
    <figure className="pv-torch pv-torch--scratch">
      <div className="pv-torch__stage" role="img" aria-label={block.alt || ctx.t("A scratch card")} aria-describedby={hint}>
        <img className="pv-torch__plain" src={play.mediaURL(block.media)} alt="" draggable={false} onLoad={coat} />
        <canvas
          ref={canvas}
          className="pv-torch__coat"
          onPointerDown={(e) => {
            if (ctx.preview) return;
            e.currentTarget.setPointerCapture(e.pointerId);
            last.current = null;
            rub(e);
          }}
          onPointerMove={(e) => {
            if (e.buttons && !ctx.preview) rub(e);
          }}
          onPointerUp={() => {
            last.current = null;
          }}
        />
      </div>
      <figcaption id={hint} className="pv-muted">
        {ctx.t("Rub the silver off with your finger.")}
      </figcaption>
    </figure>
  );
};

/**
 * A picture hidden under red noise, read through a red lens. Held, the
 * picture shows by its red channel alone, as through a red filter: red
 * noise fades towards white and what is drawn in dark blue or cyan stands
 * out. The author makes the picture that way — the message in dark blue
 * or cyan, buried under dense red and pink scribbles.
 */
const RedLens: Renderer<"torch"> = ({ block, ctx }) => {
  const id = `red-${useId().replace(/:/g, "")}`;
  const [held, setHeld] = useState(false);
  useEffect(() => {
    if (!held) return;
    const up = () => setHeld(false);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
  }, [held]);
  return (
    <figure className="pv-torch pv-torch--red">
      <svg width="0" height="0" aria-hidden="true" focusable="false" className="pv-torch__defs">
        <filter id={id} colorInterpolationFilters="sRGB">
          {/* Every channel takes the red one: the view through a red filter. */}
          <feColorMatrix type="matrix" values="1 0 0 0 0  1 0 0 0 0  1 0 0 0 0  0 0 0 1 0" />
        </filter>
      </svg>
      <div className="pv-torch__stage" role="img" aria-label={block.alt || ctx.t("A picture under red scribbles")}>
        <img className="pv-torch__plain" src={play.mediaURL(block.media)} alt="" draggable={false} style={held ? { filter: `url(#${id})` } : undefined} />
        {held && <span className="pv-torch__redlens" aria-hidden="true" />}
      </div>
      <figcaption className="pv-torch__controls">
        <button
          type="button"
          className="pv-btn pv-btn--primary"
          aria-pressed={held}
          disabled={ctx.preview}
          onPointerDown={() => setHeld(true)}
          onKeyDown={(e) => {
            if (e.key === " " || e.key === "Enter") setHeld(true);
          }}
          onKeyUp={() => setHeld(false)}
          onContextMenu={(e) => e.preventDefault()}
        >
          {ctx.t("Hold the red lens")}
        </button>
      </figcaption>
    </figure>
  );
};
