import type { ReactNode } from "react";
import type { play } from "@zonventure/api";
import type { Translate } from "../feedback.ts";
import type { MapPoint } from "../StaticMap.tsx";

/** What every block renderer may use besides its own block. */
export interface BlockContext {
  /** Where the team is; unknown until located. */
  position: MapPoint | undefined;
  t: Translate;
  /** "About 120 m away" for a zone, as the host says it. */
  placeNote: ((z: play.PlayZone) => ReactNode) | undefined;
  /** The team's name, for {{team}} in a text. */
  teamName: string | undefined;
  /** The engine's clock, in epoch ms, ticking. */
  now: number;
  /** What the team carries, for a status block. */
  items: play.ItemView[] | undefined;
  /** The step the blocks are on, and how a block acts there: a quiz pick. */
  node?: string | undefined;
  act?: ((a: play.PlayAction) => void) | undefined;
  /** An action is in flight. */
  busy?: boolean;
  /** A designer's preview: no sensors, no camera, nothing that asks permission. */
  preview?: boolean;
  /** Shows what plays out over time — a chat's messages — at once. */
  instant?: boolean;
  /** Says a block that plays out over time has finished, so what follows it shows. */
  played?: (id: string) => void;
  /**
   * A simulated device, from the designer's simulator: the compass and
   * the level read it instead of the phone's sensors, and work fully.
   */
  motion?: Motion | undefined;
  /**
   * A real event, played on the team's own phones: only then can a
   * player vote. The simulator and the designer's preview leave it off.
   */
  live?: boolean;
  /** Sends the team's picture for a photo block; absent outside a real event. */
  photo?: ((block: string, file: File) => Promise<void>) | undefined;
  /** The organisers' newest message to the team, for a bulletin block. */
  bulletin?: { text: string; created_at: string } | undefined;
}

/** How a device is held: its compass heading, and its tilt as DeviceOrientationEvent reports it. */
export interface Motion {
  /** Degrees clockwise from north that the top edge points. */
  heading: number;
  /** Front-back tilt: 0 flat, 90 upright, above 90 leaning back. */
  beta: number;
  /** Left-right tilt, -90 to 90. */
  gamma: number;
}

export type Of<K extends play.PlayBlock["kind"]> = play.PlayBlock & { kind: K };

/** Draws one kind of block. */
export type Renderer<K extends play.PlayBlock["kind"]> = (p: { block: Of<K>; ctx: BlockContext }) => ReactNode;

/** A text with {{team}} filled in; the server fills the rest. */
export function fillTeam(s: string, ctx: BlockContext): string {
  return ctx.teamName ? s.replace(/\{\{\s*team\s*\}\}/g, ctx.teamName) : s;
}
