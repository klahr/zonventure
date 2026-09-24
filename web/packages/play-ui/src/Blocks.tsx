import { useEffect, useRef, useState, type ReactNode } from "react";
import type { play } from "@zonventure/api";
import { CompassBlock } from "./Compass.tsx";
import type { Translate } from "./feedback.ts";
import { metresBetween } from "./geo.ts";
import { Markdown } from "./Markdown.tsx";
import { MediaList } from "./Media.tsx";
import { StaticMap, type MapPoint, type MapZone } from "./StaticMap.tsx";

export interface BlocksProps {
  blocks: play.PlayBlock[] | undefined;
  /** Where the team is, for a compass or a map; unknown until located. */
  position: MapPoint | undefined;
  t: Translate;
  placeNote: ((z: play.PlayZone) => ReactNode) | undefined;
}

/**
 * A puzzle's, story's or end's blocks, in the author's order. Pictures
 * and sounds next to each other share one media group, laid out as a
 * step's media were before blocks.
 */
export function Blocks({ blocks, position, t, placeNote }: BlocksProps) {
  if (!blocks?.length) return null;
  const out: ReactNode[] = [];
  let run: play.PlayMedia[] = [];
  const flush = () => {
    if (run.length) out.push(<MediaList key={`media-${out.length}`} media={run} />);
    run = [];
  };
  for (const b of blocks) {
    if (b.kind !== "image" && b.kind !== "audio") flush();
    switch (b.kind) {
      case "image":
      case "audio":
        run.push({ id: b.media, kind: b.kind, ...(b.alt ? { alt: b.alt } : {}) });
        break;
      case "text":
        if (b.text) out.push(<Markdown key={b.id} className="pv-text" text={b.text} />);
        break;
      case "compass":
        out.push(<CompassBlock key={b.id} block={b} position={position} t={t} placeNote={placeNote} />);
        break;
      case "map":
        if (b.place) out.push(<MapBlock key={b.id} place={b.place} zoom={b.zoom} position={position} />);
        break;
      default: {
        // A kind this bundle does not know yet, from a newer server: skip it.
        const unknown: never = b;
        void unknown;
      }
    }
  }
  flush();
  return <>{out}</>;
}

/** Farther than this, the team is left off the map rather than zooming out to a city. */
const NEAR_M = 3000;

/**
 * A zone on a map as wide as the step, with the team on it: when near, on
 * a map fitted to both; wherever it falls, on a map at a fixed zoom.
 */
function MapBlock({ place, zoom, position }: { place: play.PlayZone; zoom: number | undefined; position: MapPoint | undefined }) {
  const near = position && (zoom || metresBetween(position.lat, position.lng, place.lat, place.lng) <= NEAR_M);
  return <FitMap className="pv-map" zone={place} zoom={zoom} markers={near ? [position] : []} />;
}

/** A StaticMap as wide as its container, at a map's usual proportions. */
export function FitMap({ zone, markers, zoom, className }: { zone: MapZone; markers?: MapPoint[]; zoom?: number | undefined; className?: string }) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.round(e!.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return (
    <div className={className} ref={box}>
      {width > 0 && <StaticMap zone={zone} width={width} height={Math.round(width * 0.62)} zoom={zoom} {...(markers ? { markers } : {})} />}
    </div>
  );
}
