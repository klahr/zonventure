import { useId, useRef, type KeyboardEvent, type PointerEvent } from "react";
import { play } from "@zonventure/api";
import { fractionAt, nudge, type Fraction } from "../pictures.ts";
import type { Renderer } from "./context.ts";
import "./torch.css";

/**
 * A dark picture, explored with a torch that follows the finger and stays
 * where it was left. The light is a CSS mask on a second copy of the
 * picture; it moves by custom properties, not React renders, so dragging
 * stays smooth on a phone.
 */
export const TorchBlock: Renderer<"torch"> = ({ block, ctx }) => {
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
    <figure className="pv-torch">
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
        {ctx.t("Drag your finger to shine the torch.")}
      </figcaption>
    </figure>
  );
};
