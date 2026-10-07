// Palette entries: the things an author can drop onto the canvas.
//
// Deliberately richer than the seven node types. "Puzzle" alone says
// nothing about intent; "Choice: two answers, two ways" is a decision an
// author actually makes, and arriving pre-configured saves them building
// the same outputs by hand every time. Each preset is one node type with
// sensible defaults — nothing here the inspector cannot also produce.
//
// Modelled on dazyflow's quick-drop palette: drag a wire out of a port,
// release on empty canvas, pick from a searchable list, and the new node
// is created at the drop point already wired in.

import { blocksOf, withBlocks, withFixed } from "./blocks.ts";
import { freshSlug, portsOf } from "./model.ts";
import type { Graph, Node, NodeType } from "./types.ts";

export type PresetCategory = "puzzle" | "story" | "flow" | "effect" | "end";

export interface Preset {
  id: string;
  type: NodeType;
  category: PresetCategory;
  label: string;
  /** One line under the label: what it is for, not what it is. */
  hint: string;
  /** Extra words the search should match, for synonyms authors use. */
  keywords?: string;
  build: (slug: string) => Omit<Node, "slug" | "x" | "y">;
}

const answer = (port: string, label: string, value: string, points = 10) => ({
  port,
  label,
  trigger: "answer" as const,
  points,
  matchers: [{ kind: "exact" as const, value }],
});

export const PRESETS: Preset[] = [
  {
    id: "puzzle-text",
    type: "puzzle",
    category: "puzzle",
    label: "Puzzle",
    hint: "A question with a typed answer",
    keywords: "question riddle text answer",
    build: () => ({
      type: "puzzle",
      title: "Puzzle",
      puzzle: { blocks: [{ id: "b1", kind: "text", text: "" }], outputs: [answer("solved", "Solved", "")] },
    }),
  },
  {
    id: "puzzle-choice",
    type: "puzzle",
    category: "puzzle",
    label: "Choice",
    hint: "Two answers that lead two different ways",
    keywords: "branch fork decision door left right option",
    build: () => ({
      type: "puzzle",
      title: "Choice",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        outputs: [answer("a", "Option A", "", 0), answer("b", "Option B", "", 0)],
      },
    }),
  },
  {
    id: "puzzle-number",
    type: "puzzle",
    category: "puzzle",
    label: "Number, partial credit",
    hint: "Closer guesses earn more points",
    keywords: "estimate guess numeric near count how many",
    build: () => ({
      type: "puzzle",
      title: "Estimate",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        outputs: [{
          port: "solved", label: "Answered", trigger: "answer", points: 100,
          matchers: [{ kind: "near", target: 100, tolerance: 20 }],
        }],
      },
    }),
  },
  {
    // A story gated on a zone, not a puzzle: the player presses one
    // button that only works on the spot, exactly as zv's no-answer pages
    // behave today. A puzzle with a catch-all answer would make them type
    // something just to prove they arrived.
    id: "checkin",
    type: "story",
    category: "story",
    label: "Check in at a place",
    hint: "Reach a spot on the map to continue",
    keywords: "gps location geofence arrive zone place map checkpoint",
    build: () => ({ type: "story", title: "Check in", story: { blocks: [{ id: "b1", kind: "text", text: "" }], zone: "" } }),
  },
  {
    id: "story",
    type: "story",
    category: "story",
    label: "Story",
    hint: "Text to read, then continue",
    keywords: "narrative intro message text info read",
    build: () => ({ type: "story", title: "Story", story: { blocks: [{ id: "b1", kind: "text", text: "" }] } }),
  },
  {
    id: "hub-choice",
    type: "hub",
    category: "flow",
    label: "Let the team choose",
    hint: "Branches the team picks between",
    keywords: "hub stations choice open world pick menu",
    build: () => ({
      type: "hub",
      title: "Choose",
      hub: { policy: "choice", branches: [{ port: "a", label: "A" }, { port: "b", label: "B" }] },
    }),
  },
  {
    id: "hub-shuffle",
    type: "hub",
    category: "flow",
    label: "Shuffled stations",
    hint: "Every team visits all, each in its own order",
    keywords: "hub shuffle rotate stations order spread",
    build: () => ({
      type: "hub",
      title: "Stations",
      hub: {
        policy: "shuffle",
        branches: [{ port: "a", label: "Station A" }, { port: "b", label: "Station B" }, { port: "c", label: "Station C" }],
      },
    }),
  },
  {
    id: "hub-capacity",
    type: "hub",
    category: "flow",
    label: "Crowd control",
    hint: "Stations with a limit on teams at once",
    keywords: "hub capacity limit queue crowd busy stations",
    build: () => ({
      type: "hub",
      title: "Stations",
      hub: {
        policy: "capacity",
        branches: [{ port: "a", label: "Station A", capacity: 2 }, { port: "b", label: "Station B", capacity: 2 }],
      },
    }),
  },
  {
    id: "gate",
    type: "gate",
    category: "flow",
    label: "Gate",
    hint: "Go one way or another based on what the team has done",
    keywords: "condition if branch check require history solved",
    build: () => ({ type: "gate", title: "Gate", gate: { condition: { op: "" } } }),
  },
  {
    id: "puzzle-tiles",
    type: "puzzle",
    category: "puzzle",
    label: "Tile puzzle",
    hint: "Put a cut-up picture back together",
    keywords: "jigsaw picture image pieces swap painting tiles",
    build: () => ({
      type: "puzzle",
      title: "Tile puzzle",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "tiles", rows: 3, cols: 3 },
        outputs: [{ port: "solved", label: "Solved", trigger: "answer" }],
      },
    }),
  },
  {
    id: "puzzle-pick",
    type: "puzzle",
    category: "puzzle",
    label: "Multiple choice",
    hint: "Options to tap; each can lead its own way",
    keywords: "choice options buttons pick select quiz abc",
    build: () => ({
      type: "puzzle",
      title: "Multiple choice",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "choice", fields: [{ options: [{ value: "a", label: "A" }, { value: "b", label: "B" }] }] },
        outputs: [
          { port: "a", label: "A", trigger: "answer", matchers: [{ kind: "exact", value: "a" }] },
          { port: "b", label: "B", trigger: "answer", matchers: [{ kind: "exact", value: "b" }] },
        ],
      },
    }),
  },
  {
    id: "puzzle-form",
    type: "puzzle",
    category: "puzzle",
    label: "Marked form",
    hint: "Several questions, marked together — an accusation",
    keywords: "accusation solve case form quiz score marked dropdowns who what where",
    build: () => ({
      type: "puzzle",
      title: "Solve the case",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "choice", fields: [{ label: "Who did it?", options: [{ value: "a", label: "A" }, { value: "b", label: "B" }], answer: "a" }] },
        outputs: [{ port: "done", label: "Sent in", trigger: "answer", points: 100 }],
      },
    }),
  },
  {
    id: "puzzle-lock",
    type: "puzzle",
    category: "puzzle",
    label: "Combination lock",
    hint: "Turn wheels of pictures or words to the right combination",
    keywords: "lock wheels code combination padlock symbols animals slot",
    build: () => ({
      type: "puzzle",
      title: "Combination lock",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "lock", wheels: 3, start: [1, 2, 0], symbols: [{ value: "1", label: "1" }, { value: "2", label: "2" }, { value: "3", label: "3" }] },
        outputs: [{ port: "open", label: "Open", trigger: "answer", matchers: [{ kind: "exact", value: "1|1|1" }] }],
        responses: [{ text: "It does not budge." }],
      },
    }),
  },
  {
    id: "puzzle-items",
    type: "puzzle",
    category: "puzzle",
    label: "Item slots",
    hint: "Place items from the backpack in the right slots to get through",
    keywords: "items inventory backpack key gem place put slots lock door pedestal use",
    build: () => ({
      type: "puzzle",
      title: "Item slots",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "items", slots: [{ label: "", item: "" }, { label: "", item: "" }] },
        // A way out for a team that has yet to find the items.
        outputs: [{ port: "open", label: "Open", trigger: "answer" }, { port: "later", label: "Come back later", trigger: "missing" }],
        responses: [{ text: "Nothing happens. Something is in the wrong place." }],
      },
    }),
  },
  {
    id: "puzzle-search",
    type: "puzzle",
    category: "puzzle",
    label: "Search by place",
    hint: "The team searches where it stands; each place finds something else",
    keywords: "scanner gps grid sector location area search find where",
    build: () => ({
      type: "puzzle",
      title: "Scanner",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        answer: { kind: "search" },
        repeatable: true,
        outputs: [{ port: "found", label: "Found", trigger: "answer", zone: "" }],
        responses: [{ text: "Nothing here." }],
      },
    }),
  },
  {
    id: "puzzle-questions",
    type: "puzzle",
    category: "puzzle",
    label: "Quiz post",
    hint: "Quiz-walk questions answered 1, X or 2 — with a place, time limit or hints",
    keywords: "quiz walk 1x2 questions tipspromenad trivia post tiebreak",
    build: () => ({
      type: "puzzle",
      title: "Quiz post",
      puzzle: {
        blocks: [{ id: "b1", kind: "quiz", text: "", options: ["", "", ""] }],
        answer: { kind: "questions" },
        outputs: [{ port: "done", label: "Answered", trigger: "answer" }],
      },
    }),
  },
  {
    id: "puzzle-open",
    type: "puzzle",
    category: "puzzle",
    label: "Open question",
    hint: "Any answer moves on, carrying it to a crossroads",
    keywords: "anything free text response poll opinion switch",
    build: () => ({
      type: "puzzle",
      title: "Open question",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "" }],
        outputs: [{ port: "answered", label: "Answered", trigger: "answer", matchers: [{ kind: "anything" }] }],
      },
    }),
  },
  {
    id: "start",
    type: "start",
    category: "flow",
    label: "Another start",
    hint: "A second place to begin; teams choose where they start",
    keywords: "start begin entry meeting point location place",
    build: () => ({ type: "start", title: "Start" }),
  },
  {
    id: "switch",
    type: "switch",
    category: "flow",
    label: "Crossroads",
    hint: "Send the team different ways depending on what they answered",
    keywords: "switch case route path fork response answer branch depends",
    build: () => ({
      type: "switch",
      title: "Crossroads",
      switch: {
        cases: [
          { port: "a", label: "A" },
          { port: "b", label: "B" },
        ],
      },
    }),
  },
  {
    id: "effect-score",
    type: "effect",
    category: "effect",
    label: "Award points",
    hint: "Add to the team's score",
    keywords: "score points bonus reward",
    build: () => ({ type: "effect", title: "Award points", effect: { kind: "score", points: 10 } }),
  },
  {
    id: "effect-item",
    type: "effect",
    category: "effect",
    label: "Give an item",
    hint: "Something the team can see in its inventory",
    keywords: "item inventory key object collect give",
    build: () => ({ type: "effect", title: "Give item", effect: { kind: "grant_item", item: "", qty: 1 } }),
  },
  {
    id: "effect-counter",
    type: "effect",
    category: "effect",
    label: "Change a counter",
    hint: "A number every team in the event shares",
    keywords: "counter shared global tally variable",
    build: () => ({ type: "effect", title: "Counter", effect: { kind: "counter", counter: "", op: "add", value: 1 } }),
  },
  {
    id: "effect-journal",
    type: "effect",
    category: "effect",
    label: "Unlock a journal entry",
    hint: "Lore the team can read back later",
    keywords: "journal lore notebook diary unlock",
    build: () => ({ type: "effect", title: "Journal", effect: { kind: "journal", journal_entry: "" } }),
  },
  {
    id: "effect-email",
    type: "effect",
    category: "effect",
    label: "Send an email",
    hint: "Queued and sent after the move is saved",
    keywords: "email mail notify message",
    build: () => ({ type: "effect", title: "Email", effect: { kind: "email", subject: "", body: "" } }),
  },
  {
    id: "effect-webhook",
    type: "effect",
    category: "effect",
    label: "Call a webhook",
    hint: "Tell your own system something happened",
    keywords: "webhook http integration api notify",
    build: () => ({ type: "effect", title: "Webhook", effect: { kind: "webhook", url: "" } }),
  },
  {
    id: "effect-theme",
    type: "effect",
    category: "effect",
    label: "Change the theme",
    hint: "Restyle the player's screen from here on",
    keywords: "theme style colour color mood skin",
    build: () => ({ type: "effect", title: "Theme", effect: { kind: "theme", theme: "" } }),
  },
  {
    id: "end",
    type: "end",
    category: "end",
    label: "Finish",
    hint: "The adventure is complete",
    keywords: "end finish complete done win",
    build: () => ({ type: "end", title: "Finish", end: { outcome: "complete" } }),
  },
  {
    id: "end-fail",
    type: "end",
    category: "end",
    label: "Fail",
    hint: "The run ends without success",
    keywords: "end fail lose game over",
    build: () => ({ type: "end", title: "Failed", end: { outcome: "fail" } }),
  },
];

/** Build a node from a preset at a canvas position. */
export function nodeFromPreset(g: Graph, preset: Preset, at: { x: number; y: number }): Node {
  const slug = freshSlug(g, preset.type);
  return withFixed({ ...preset.build(slug), slug, x: Math.round(at.x), y: Math.round(at.y) } as Node);
}

/** What an author may fill in when adding a step, beyond its preset. */
export interface StepFill {
  title?: string;
  text?: string;
  answer?: string;
}

/** The fields a preset asks for after it is picked; empty for none. */
export type FillField = keyof StepFill;
export function fillFields(p: Preset): FillField[] {
  if (p.type === "puzzle") return typedAnswer(p) ? ["title", "text", "answer"] : ["title", "text"];
  if (p.type === "story" || p.type === "end") return ["title", "text"];
  if (p.type === "start") return [];
  return ["title"];
}

/** The preset whose puzzle takes one typed answer, which `answer` fills. */
const typedAnswer = (p: Preset) => p.id === "puzzle-text";

/**
 * A node built from a preset with what the author filled in: the title,
 * the first text block's text (a text block is added when there is none),
 * and a typed puzzle's accepted answer. Blank fields keep the preset's.
 */
export function fillStep(n: Node, p: Preset, fill: StepFill): Node {
  const title = fill.title?.trim();
  const text = fill.text?.trim();
  const answer = fill.answer?.trim();
  let out: Node = title ? { ...n, title } : n;
  if (text && (out.puzzle || out.story || out.end)) {
    const bs = blocksOf(out);
    const i = bs.findIndex((b) => b.kind === "text");
    let id = "b1";
    while (bs.some((b) => b.id === id)) id += "-";
    out = withBlocks(out, i >= 0 ? bs.map((b, j) => (j === i ? { ...b, text } : b)) : [...bs, { id, kind: "text", text }]);
  }
  if (answer && typedAnswer(p) && out.puzzle?.outputs?.[0]) {
    const outputs = out.puzzle.outputs.map((o, j) => (j === 0 ? { ...o, matchers: [{ kind: "exact" as const, value: answer }] } : o));
    out = { ...out, puzzle: { ...out.puzzle, outputs } };
  }
  return out;
}

/**
 * Which end of a wire the author dragged. Dragging from an output needs
 * something that takes an input; dragging backwards from an input needs
 * something with an output.
 */
export type DragFrom = { side: "output"; node: Node; port: string } | { side: "input"; node: Node };

/** The presets that make sense to create for a drag, or all when there is none. */
export function presetsFor(from: DragFrom | null): Preset[] {
  if (from?.side !== "input") return PRESETS; // every preset takes an input
  return PRESETS.filter((p) => p.type !== "end"); // an end has no output to wire in
}

/**
 * Suggestions shown first, for the common next step.
 *
 * dazyflow learns these from what other flows connect; there is no usage
 * data for adventures yet, so these are the handful of moves authors make
 * most, by where the wire came from. Worth replacing with learned
 * adjacency once there are enough saved graphs to learn from.
 */
export function suggestionsFor(from: DragFrom | null): string[] {
  if (!from) return ["puzzle-text", "story", "puzzle-choice"];
  if (from.side === "input") return ["puzzle-text", "story", "start"];
  const n = from.node;
  switch (n.type) {
    case "start":
      return ["story", "puzzle-text", "checkin"];
    case "puzzle":
      return ["puzzle-text", "switch", "story", "effect-score", "end"];
    case "story":
      return ["puzzle-text", "story", "effect-score", "end"];
    case "switch":
      return ["puzzle-text", "story", "end"];
    case "hub":
      return from.port === "done" ? ["gate", "puzzle-text", "end"] : ["puzzle-text", "checkin", "story"];
    case "gate":
      return ["puzzle-text", "story", "end"];
    case "effect":
      return ["puzzle-text", "story", "gate"];
    default:
      return [];
  }
}

/**
 * Score a preset against a search query; 0 means no match.
 *
 * Label beats keywords beats hint, and a prefix beats a substring, so
 * "gat" puts Gate first rather than anything whose hint mentions a gate.
 * Every word of the query must match somewhere. With tr, the preset's
 * label, hint and keywords in the author's language count too, so a
 * Swedish author finds "Byt tema" by "tema" — the English words still
 * work as well.
 */
export function scorePreset(p: Preset, query: string, tr?: (s: string) => string): number {
  const words = query.toLowerCase().trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return 1;
  const english = { label: p.label, keywords: p.keywords ?? "", hint: p.hint };
  if (!tr) return scoreWords(english, words);
  const local = { label: tr(p.label), keywords: p.keywords ? tr(p.keywords) : "", hint: tr(p.hint) };
  return Math.max(scoreWords(english, words), scoreWords(local, words));
}

function scoreWords(f: { label: string; keywords: string; hint: string }, words: string[]): number {
  const label = f.label.toLowerCase();
  const keywords = f.keywords.toLowerCase();
  const hint = f.hint.toLowerCase();
  let total = 0;
  for (const w of words) {
    let s = 0;
    if (label.startsWith(w)) s = 100;
    else if (label.split(/[^\p{L}\p{N}]+/u).some((part) => part.startsWith(w))) s = 80;
    else if (label.includes(w)) s = 60;
    else if (keywords.split(/\s+/).some((k) => k.startsWith(w))) s = 40;
    else if (hint.includes(w)) s = 20;
    if (s === 0) return 0;
    total += s;
  }
  return total;
}

/**
 * Existing nodes a dragged output could be wired to, for "connect to an
 * existing step" — loops back to a hub are common, and the hub's handle is
 * often far off screen.
 */
export function existingTargets(g: Graph, from: DragFrom | null): Node[] {
  if (!from || from.side !== "output") return [];
  return g.nodes.filter((n) => n.type !== "start" && n.slug !== from.node.slug);
}

/** The port on a new node that a backward drag should wire from. */
export function firstPort(n: Node): string | undefined {
  return portsOf(n)[0];
}
