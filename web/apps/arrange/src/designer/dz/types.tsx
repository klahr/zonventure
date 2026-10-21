import { BookOpen, DoorOpen, Flag, GitFork, GitMerge, Puzzle, Signpost, Sparkles, Split, Trophy, type LucideIcon } from "lucide-react";
import type { NodeType } from "@zonventure/graph";

/**
 * Each step type's icon, name and one-line purpose — what dazyflow's
 * DropIcon and drop description do for its catalogue. Shown on cards, in
 * the inspector header and in its (i) help.
 */
export const STEP: Record<NodeType, { icon: LucideIcon; label: string; about: string }> = {
  start: { icon: Flag, label: "Start", about: "Where every team begins. Each adventure has exactly one." },
  puzzle: { icon: Puzzle, label: "Puzzle", about: "A question the team answers. Each way out is a route: a right answer, a wrong one, running out of time or skipping." },
  story: { icon: BookOpen, label: "Story", about: "Something to read before moving on — optionally only once the team is at a place." },
  hub: { icon: Split, label: "Hub", about: "A set of branches the team works through: chosen by the team, in order, shuffled, spread out or limited per branch." },
  gate: { icon: DoorOpen, label: "Gate", about: "Sends the team one way or another based on what it has done so far." },
  effect: { icon: Sparkles, label: "Effect", about: "Changes something — points, an item, a counter, a journal page — and moves straight on." },
  switch: { icon: Signpost, label: "Crossroads", about: "Sends the team different ways depending on what it answered. Teams never see it." },
  split: { icon: GitFork, label: "Split up", about: "The team divides into groups, one per path, each walking its own steps on its own phones until they meet again at a join. A path's role takes its player; everyone else is shared out evenly." },
  join: { icon: GitMerge, label: "Meet again", about: "Where a split's groups meet. Each group waits here until the last arrives, then the team goes on together with everything every group found." },
  end: { icon: Trophy, label: "End", about: "The adventure is over, as a success or a failure." },
};

/** Steps a player sees. The others run the moment a team reaches them. */
export const VISIBLE: ReadonlySet<string> = new Set<NodeType>(["puzzle", "story", "hub", "split", "join", "end"]);
