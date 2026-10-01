import {
  BookMarked,
  GitBranch,
  Hash,
  KeyRound,
  Languages,
  Lightbulb,
  ListChecks,
  MapPin,
  MessageSquareReply,
  Package,
  Palette,
  Route,
  Shuffle,
  SlidersHorizontal,
  Smartphone,
  Sparkles,
  Timer,
  Trophy,
  Users,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import type { Trigger } from "@zonventure/graph";
import { ICON } from "./ui.tsx";

/**
 * The concepts an adventure is built from, each with one colour
 * (--c-<concept> in @zonventure/ui's tokens) used wherever it appears:
 * the inspector's parts, the adventure's resources, chips, the maps.
 * Step types have their own hues (--type-*); where a step is mostly one
 * concept — a story is content, a crossroads is flow — they agree.
 */
export type Concept = "content" | "answer" | "hint" | "flow" | "logic" | "place" | "time" | "effect" | "item" | "journal";

/** Each inspector part's concept and icon, by part id (see partsOf). */
export const PART: Record<string, { concept: Concept; icon: LucideIcon }> = {
  content: { concept: "content", icon: Smartphone },
  text: { concept: "content", icon: Smartphone },
  answer: { concept: "answer", icon: KeyRound },
  source: { concept: "answer", icon: KeyRound },
  replies: { concept: "answer", icon: MessageSquareReply },
  hints: { concept: "hint", icon: Lightbulb },
  routes: { concept: "flow", icon: Route },
  branches: { concept: "flow", icon: GitBranch },
  paths: { concept: "flow", icon: Waypoints },
  setup: { concept: "flow", icon: Shuffle },
  rules: { concept: "logic", icon: SlidersHorizontal },
  condition: { concept: "logic", icon: ListChecks },
  effect: { concept: "effect", icon: Sparkles },
  place: { concept: "place", icon: MapPin },
  journal: { concept: "journal", icon: BookMarked },
};

/** What a puzzle route is taken on, as a concept: its port on the canvas wears the colour. */
export const TRIGGER: Record<Trigger, Concept> = {
  answer: "answer",
  timeout: "time",
  skipped: "logic",
  attempts: "logic",
  missing: "item",
};

/** The adventure-wide resources, as the settings list them. */
export const RESOURCE = {
  zones: { concept: "place", icon: MapPin },
  items: { concept: "item", icon: Package },
  counters: { concept: "effect", icon: Hash },
  time: { concept: "time", icon: Timer },
  journal: { concept: "journal", icon: BookMarked },
  roles: { concept: "content", icon: Users },
  score: { concept: "answer", icon: Trophy },
  languages: { concept: "content", icon: Languages },
  look: { concept: "content", icon: Palette },
} as const satisfies Record<string, { concept: Concept; icon: LucideIcon }>;

/** A concept's icon on a tile tinted in its colour. */
export function ConceptIcon({ concept, icon: Icon, size = ICON.sm }: { concept: Concept; icon: LucideIcon; size?: number }) {
  return (
    <span className={`concept-icon concept--${concept}`} aria-hidden="true">
      <Icon size={size} />
    </span>
  );
}

/** A concept's colour as a hex string, for canvases CSS cannot reach (MapLibre layers). */
export function conceptColor(concept: Concept, fallback: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(`--c-${concept}`).trim() || fallback;
}
