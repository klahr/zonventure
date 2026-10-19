import { useEffect, useRef, useState } from "react";
import type { Translate } from "../feedback.ts";
import { bearing, metresBetween, nearestTurn } from "../geo.ts";
import type { BlockContext, Of } from "./context.ts";

type IOSOrientation = typeof DeviceOrientationEvent & { requestPermission?: () => Promise<"granted" | "denied"> };
type OrientationReading = DeviceOrientationEvent & { webkitCompassHeading?: number };

/**
 * Which way the phone's top edge points, in degrees from north, or null
 * where the device has no compass (a laptop) or has not reported yet.
 *
 * iOS reports webkitCompassHeading, but only after the page asks from a
 * tap; `ask` is set until then. Android reports an absolute alpha on
 * deviceorientationabsolute, counter-clockwise, and neither turns with
 * the screen, so a landscape phone adds the screen's angle.
 */
export function useHeading(enabled = true): { heading: number | null; ask: (() => void) | null } {
  const [heading, setHeading] = useState<number | null>(null);
  const [granted, setGranted] = useState(false);
  const Orientation = typeof DeviceOrientationEvent === "undefined" ? undefined : (DeviceOrientationEvent as IOSOrientation);
  const needsAsk = typeof Orientation?.requestPermission === "function";
  const awake = useAwake();

  useEffect(() => {
    if (!enabled || !Orientation || (needsAsk && !granted)) return;
    const onReading = (e: Event) => {
      const r = e as OrientationReading;
      let h: number | null = null;
      if (typeof r.webkitCompassHeading === "number") h = r.webkitCompassHeading;
      else if ((r.absolute || e.type === "deviceorientationabsolute") && r.alpha != null) h = 360 - r.alpha;
      if (h == null) return;
      const screenAngle = typeof screen !== "undefined" ? (screen.orientation?.angle ?? 0) : 0;
      const next = (h + screenAngle + 360) % 360;
      // A reading comes many times a second; redraw only for a visible turn.
      setHeading((prev) => (prev != null && Math.abs(nearestTurn(prev, next) - prev) < 2 ? prev : next));
    };
    const kind = "ondeviceorientationabsolute" in window ? "deviceorientationabsolute" : "deviceorientation";
    window.addEventListener(kind, onReading);
    return () => window.removeEventListener(kind, onReading);
  }, [enabled, Orientation, needsAsk, granted, awake]);

  const ask =
    enabled && needsAsk && !granted
      ? () => {
          void Orientation!.requestPermission!()
            .then((r) => setGranted(r === "granted"))
            .catch(() => setGranted(false));
        }
      : null;
  return { heading, ask };
}

/**
 * Counts the times the page has come back into view. A phone stops a
 * hidden page's sensors and does not always start them again for the
 * listener it had: one added afresh gets readings.
 */
export function useAwake(): number {
  const [awake, setAwake] = useState(0);
  useEffect(() => {
    const onVis = () => {
      if (document.visibilityState === "visible") setAwake((n) => n + 1);
    };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, []);
  return awake;
}

/**
 * A needle pointing at a zone, with how far it is unless the author hid
 * that. Without a compass the dial is north-up and says so; without a
 * position it waits for one.
 */
export function CompassBlock({ block, ctx }: { block: Of<"compass">; ctx: BlockContext }) {
  const { position, t, placeNote } = ctx;
  const device = useHeading(!ctx.preview && !ctx.motion);
  const { heading, ask } = ctx.motion ? { heading: ctx.motion.heading, ask: null } : device;
  const turn = useRef(0);
  const place = block.place;
  if (!place) return null;

  const target = position ? bearing(position.lat, position.lng, place.lat, place.lng) : null;
  // Arrived only once the server agrees: its positions, not this fix,
  // decide an answer, and they lag the phone by a ping or two.
  const there = !!position && (place.polygon?.length ?? 0) < 3 && metresBetween(position.lat, position.lng, place.lat, place.lng) <= place.radius_m && block.here !== false;
  if (target != null) turn.current = nearestTurn(turn.current, target - (heading ?? 0));

  return (
    <CompassFace
      name={place.name || place.slug}
      angle={target != null ? turn.current : null}
      northUp={heading == null}
      there={there}
      quiet={!!ctx.arrival}
      t={t}
      distance={!block.hide_distance && placeNote ? placeNote(place) : null}
      note={target != null && heading == null && !ask ? t("No compass on this device: north is up.") : null}
      ask={ask}
    />
  );
}

/** The compass as drawn, for the live block and the designer's preview. */
export function CompassFace({ name, angle, northUp, there = false, quiet = false, t, distance, note, ask }: {
  name: string;
  /** Where the needle points, in degrees clockwise from the dial's top; null hides it. */
  angle: number | null;
  northUp: boolean;
  there?: boolean;
  /** Leaves saying the team is there to the step's own box: the star alone shows it. */
  quiet?: boolean;
  t: Translate;
  distance?: React.ReactNode;
  note?: React.ReactNode;
  ask?: (() => void) | null;
}) {
  return (
    <figure className="pv-compass">
      <div className={"pv-compass__dial" + (northUp ? " is-north-up" : "")} aria-hidden="true">
        {northUp && <span className="pv-compass__north">N</span>}
        {angle != null && !there && <span className="pv-compass__needle" style={{ transform: `rotate(${angle}deg)` }} />}
        {there && <span className="pv-compass__here">★</span>}
      </div>
      <figcaption>
        <span className="pv-compass__to">{there ? (quiet ? name : t("You are at {{place}}.", { place: name })) : t("Towards {{place}}", { place: name })}</span>
        {!there && distance && <span className="pv-compass__note">{distance}</span>}
        {note && <span className="pv-compass__note">{note}</span>}
        {ask && (
          <button type="button" className="pv-btn pv-compass__ask" onClick={ask}>
            {t("Turn on the compass")}
          </button>
        )}
      </figcaption>
    </figure>
  );
}
