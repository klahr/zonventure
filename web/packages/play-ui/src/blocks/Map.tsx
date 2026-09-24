import { useEffect, useRef, useState } from "react";
import { metresBetween } from "../geo.ts";
import { StaticMap, type MapPin, type MapPoint, type MapZone } from "../StaticMap.tsx";
import type { Renderer } from "./context.ts";

/** Farther than this, a fitted map leaves the team off rather than zooming out to a city. */
const NEAR_M = 3000;

/**
 * A zone on a map as wide as the step, with the author's pins and — unless
 * hidden — the team: on a fitted map when near, on a fixed zoom wherever
 * it falls in the frame.
 */
export const MapBlock: Renderer<"map"> = ({ block, ctx }) => {
  const place = block.place;
  if (!place) return null;
  const p = ctx.position;
  const team = !block.hide_team && p && (block.zoom || metresBetween(p.lat, p.lng, place.lat, place.lng) <= NEAR_M) ? p : undefined;
  return <FitMap className="pv-map" zone={place} zoom={block.zoom} team={team} pins={block.markers ?? []} />;
};

/** A StaticMap as wide as its container, at a map's usual proportions. */
export function FitMap({ zone, team, pins, zoom, className, onPick }: { zone: MapZone; team?: MapPoint | undefined; pins?: MapPin[]; zoom?: number | undefined; className?: string; onPick?: ((p: MapPoint) => void) | undefined }) {
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
      {width > 0 && <StaticMap zone={zone} width={width} height={Math.round(width * 0.62)} zoom={zoom} team={team} onPick={onPick} {...(pins ? { pins } : {})} />}
    </div>
  );
}
