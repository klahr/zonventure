import { useEffect, useState, type MouseEvent } from "react";
import { play } from "@zonventure/api";
import { nearSpot } from "../gadgets.ts";
import { fractionAt } from "../pictures.ts";
import type { Renderer } from "./context.ts";
import "./gadgets.css";

/**
 * Two pictures to compare: a tap near a difference on either marks it on
 * both, with what it is. What this phone found is kept for the session,
 * on this phone only; nothing is graded.
 */
export const SpotDiffBlock: Renderer<"spotdiff"> = ({ block, ctx }) => {
  const { t } = ctx;
  const spots = block.spots ?? [];
  const key = `zv.spotdiff:${ctx.node ?? ""}:${block.id}`;
  const [found, setFound] = useState<number[]>(() => load(key));
  const [last, setLast] = useState<number | null>(null);

  useEffect(() => {
    try {
      sessionStorage.setItem(key, JSON.stringify(found));
    } catch {
      // A phone that keeps nothing still plays; it just forgets on reload.
    }
  }, [key, found]);

  const tap = (e: MouseEvent<HTMLDivElement>) => {
    const i = nearSpot(fractionAt(e.clientX, e.clientY, e.currentTarget.getBoundingClientRect()), spots);
    if (i < 0) return;
    setLast(i);
    setFound((f) => (f.includes(i) ? f : [...f, i]));
  };

  const picture = (media: number | undefined, which: string) =>
    media ? (
      <div className="pv-spotdiff__pic" onClick={tap} role="img" aria-label={`${block.alt || t("A picture")} (${which})`}>
        <img src={play.mediaURL(media)} alt="" draggable={false} />
        {found.map((i) => {
          const s = spots[i];
          return s ? <span key={i} className="pv-spotdiff__ring" style={{ left: `${s.x * 100}%`, top: `${s.y * 100}%` }} aria-hidden="true" /> : null;
        })}
      </div>
    ) : null;

  const all = spots.length > 0 && found.length >= spots.length;
  return (
    <figure className="pv-spotdiff">
      <div className="pv-spotdiff__pair">
        {picture(block.media, t("first"))}
        {picture(block.other, t("second"))}
      </div>
      <figcaption className="pv-gadget__caption">
        <span className={all ? "pv-gadget__cheer" : "pv-gadget__hint"} role="status">
          {all ? t("All {{n}} found!", { n: spots.length }) : t("Found {{n}} of {{m}}", { n: found.length, m: spots.length })}
        </span>
        {last != null && spots[last]?.text && <span className="pv-gadget__note">{spots[last]!.text}</span>}
        {!all && <span className="pv-gadget__note">{t("Tap what differs, on either picture.")}</span>}
      </figcaption>
    </figure>
  );
};

function load(key: string): number[] {
  try {
    const v = JSON.parse(sessionStorage.getItem(key) ?? "[]") as unknown;
    return Array.isArray(v) ? v.filter((n): n is number => Number.isInteger(n)) : [];
  } catch {
    return [];
  }
}
