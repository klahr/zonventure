import { Fragment, useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { play } from "@zonventure/api";
import { MarkdownInline } from "../Markdown.tsx";
import { bubbleSide } from "../pictures.ts";
import { fillTeam, type Of, type Renderer } from "./context.ts";
import "./hotspots.css";

/**
 * A picture with details to find: a pulsing marker on each, and its
 * caption in a bubble beside it when tapped. One bubble at a time; a tap
 * anywhere else, or on the same marker, closes it. With a mouse, hovering
 * a marker shows its caption too, until the pointer leaves it.
 */
export const HotspotsBlock: Renderer<"hotspots"> = ({ block, ctx }) => {
  const spots = block.spots ?? [];
  const [open, setOpen] = useState<number | null>(null);
  const [hover, setHover] = useState<number | null>(null);
  const shown = open ?? hover;
  const root = useRef<HTMLElement>(null);
  const id = useId();

  useEffect(() => {
    if (open === null) return;
    const close = (e: Event) => {
      const el = e.target as Element | null;
      // A marker toggles itself, and a bubble stays open so its text can be selected.
      if (el?.closest?.(".pv-hotspots__spot, .pv-hotspots__bubble") && root.current?.contains(el)) return;
      setOpen(null);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      root.current?.querySelector<HTMLElement>(`[aria-controls="${id}-${open}"]`)?.focus();
      setOpen(null);
    };
    document.addEventListener("click", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("click", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open, id]);

  return (
    <figure className="pv-hotspots" ref={root}>
      <div className="pv-hotspots__stage">
        <img className="pv-hotspots__img" src={play.mediaURL(block.media)} alt={block.alt ?? ""} draggable={false} />
        {spots.map((s, i) => (
          // The caption follows its marker in the DOM, so a screen reader reaches it next.
          <Fragment key={i}>
            <button
              type="button"
              className={"pv-hotspots__spot" + (open === i ? " is-open" : "")}
              style={{ left: `${s.x * 100}%`, top: `${s.y * 100}%` }}
              aria-label={ctx.t("Detail {{n}} of {{count}}", { n: i + 1, count: spots.length })}
              aria-expanded={open === i}
              aria-controls={`${id}-${i}`}
              onClick={() => setOpen(open === i ? null : i)}
              onPointerEnter={(e) => e.pointerType === "mouse" && setHover(i)}
              onPointerLeave={() => setHover(null)}
            />
            {shown === i && <Bubble id={`${id}-${i}`} spot={s} text={fillTeam(s.text, ctx)} />}
          </Fragment>
        ))}
      </div>
    </figure>
  );
};

type Spot = NonNullable<Of<"hotspots">["spots"]>[number];

function Bubble({ id, spot, text }: { id: string; spot: Spot; text: string }) {
  const side = bubbleSide(spot);
  const style: CSSProperties = side.v === "above" ? { bottom: `${(1 - spot.y) * 100}%` } : { top: `${spot.y * 100}%` };
  if (side.h === "end") style.right = `${(1 - spot.x) * 100}%`;
  else style.left = `${spot.x * 100}%`;
  return (
    <div id={id} className={`pv-hotspots__bubble is-${side.v} is-${side.h}`} style={style}>
      <MarkdownInline text={text} />
    </div>
  );
}
