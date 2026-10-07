import { connect, insertAfter, insertionPoint, newNode, nodeFromPreset, PRESETS, type Block, type Graph, type Node } from "@zonventure/graph";

/**
 * The designer tutorial: Erik's stolen elephants, built step by step
 * with a guide beside the canvas. What is done is read off the draft
 * where it can be — a zone exists, a puzzle has an answer — and
 * remembered where it can't: Test play was opened, the look was changed.
 * Any step can also be ticked off by hand.
 */
export const TUTORIAL_STEPS = ["welcome", "zone", "checkin", "puzzle", "hint", "choice", "finish", "look", "test", "publish"] as const;
export type TutorialStep = (typeof TUTORIAL_STEPS)[number];

/** Per adventure, in this browser: a convenience, never state that matters. */
export interface TutorialState {
  /** The welcome's text when the tutorial began, to see it rewritten. */
  baseline: string;
  /** Ticked by hand, or done by doing (test, look). */
  done: TutorialStep[];
  collapsed?: boolean;
}

export interface TutorialFacts {
  graph: Graph;
  errors: number;
  published: boolean;
}

const key = (slug: string) => "zv_tutorial:" + slug;

export function loadTutorial(slug: string): TutorialState | null {
  try {
    const s = JSON.parse(localStorage.getItem(key(slug)) ?? "null") as TutorialState | null;
    return s && Array.isArray(s.done) ? s : null;
  } catch {
    return null;
  }
}

export function saveTutorial(slug: string, s: TutorialState | null) {
  try {
    if (s) localStorage.setItem(key(slug), JSON.stringify(s));
    else localStorage.removeItem(key(slug));
  } catch {
    /* private mode: the tutorial lasts this visit */
  }
}

const textOf = (blocks: Block[] | undefined) =>
  (blocks ?? [])
    .flatMap((b) => (b.kind === "text" && !b.disabled ? [b.text ?? ""] : []))
    .join("\n")
    .trim();

/** The first story a team reads: the one a start leads to. */
export function welcomeStep(g: Graph): Node | undefined {
  const starts = new Set(g.nodes.filter((n) => n.type === "start").map((n) => n.slug));
  const to = g.edges.find((e) => starts.has(e.from))?.to;
  const first = g.nodes.find((n) => n.slug === to);
  return first?.type === "story" ? first : g.nodes.find((n) => n.type === "story");
}

export const welcomeText = (g: Graph) => {
  const w = welcomeStep(g);
  return w ? textOf(w.story?.blocks) : "";
};

/** What a story or puzzle gives the team to read; "" for nothing yet. */
export const readOf = (n: Node) => textOf(n.story?.blocks ?? n.puzzle?.blocks);

/** A story the team can only pass at a place of the adventure's. */
export function checkInStep(g: Graph): Node | undefined {
  const zones = new Set((g.zones ?? []).map((z) => z.slug));
  return g.nodes.find((n) => n.type === "story" && !!n.story?.zone && zones.has(n.story.zone));
}

/** A story or puzzle a team would find nothing to read on. */
export const emptyStep = (g: Graph) => g.nodes.find((n) => (n.type === "story" || n.type === "puzzle") && readOf(n) === "");

/** The answers a puzzle takes that a team can actually type. */
const answers = (n: Node) =>
  (n.puzzle?.outputs ?? []).filter(
    (o) => o.trigger === "answer" && (o.matchers ?? []).some((m) => m.kind === "anything" || (m.value ?? "").trim() !== "" || (m.members ?? []).length > 0 || m.low !== undefined || m.target !== undefined),
  );

export function puzzleStep(g: Graph): Node | undefined {
  return g.nodes.find((n) => n.type === "puzzle" && answers(n).length > 0);
}

/** A choice not yet asked in words still counts as the one to open. */
const choiceCandidates = (g: Graph) => g.nodes.filter((n) => n.type === "switch" || (n.type === "puzzle" && answers(n).length >= 2));

/** A puzzle whose two or more answers each lead somewhere, or a crossroads. */
export function choiceStep(g: Graph): Node | undefined {
  const wired = (n: Node, port: string) => g.edges.some((e) => e.from === n.slug && e.from_port === port);
  return choiceCandidates(g).find((n) => (n.type === "switch" ? (n.switch?.cases ?? []).length > 0 : answers(n).every((o) => wired(n, o.port))));
}

/** The choice being built, wired or not. */
export const choiceDraft = (g: Graph) => choiceCandidates(g)[0];

/** Which steps are done, read off the draft and what was remembered. */
export function tutorialProgress(s: TutorialState, f: TutorialFacts): Record<TutorialStep, boolean> {
  const g = f.graph;
  const by = new Set(s.done);
  const welcome = welcomeText(g);
  const reached = new Set(g.edges.map((e) => e.to));
  const checkIn = checkInStep(g);
  const c = choiceStep(g);
  const choice = !!c && (c.type === "switch" || readOf(c) !== "");
  const read: Record<TutorialStep, boolean> = {
    welcome: welcome !== "" && welcome !== s.baseline.trim(),
    zone: (g.zones ?? []).length > 0,
    // Done once there is something to read too: a page with only a name
    // leaves the team guessing.
    checkin: !!checkIn && readOf(checkIn) !== "",
    puzzle: g.nodes.some((n) => n.type === "puzzle" && answers(n).length > 0 && readOf(n) !== ""),
    hint: g.nodes.some((n) => n.type === "puzzle" && (n.puzzle?.hints ?? []).some((h) => h.text.trim() !== "")),
    choice,
    // Only once there is something to tie up: a new adventure starts out whole.
    finish: choice && f.errors === 0 && !emptyStep(g) && g.nodes.some((n) => n.type === "end" && reached.has(n.slug)),
    look: false,
    test: false,
    publish: f.published,
  };
  return Object.fromEntries(TUTORIAL_STEPS.map((x) => [x, read[x] || by.has(x)])) as Record<TutorialStep, boolean>;
}

/**
 * A step put after `after`, as the outline's add buttons do: on a phone,
 * where nothing is dragged into place.
 */
export function addStepAfter(g: Graph, after: string | undefined, type: "puzzle" | "story"): { graph: Graph; slug: string } | null {
  const at = insertionPoint(g, after ?? null);
  if (!at) return null;
  const node = newNode(g, type, { x: 0, y: 0 });
  return { graph: insertAfter(g, at, node), slug: node.slug };
}

/**
 * The tutorial's choice, ready-made for a phone, where its two ways can't
 * be wired by hand: answer one goes on to where the puzzle led, answer
 * two to a story that sends the team back to the choice.
 */
export function addChoiceAfter(
  g: Graph,
  after: string | undefined,
  words: { title: string; left: string; right: string; back: string; backText: string },
): { graph: Graph; slug: string } | null {
  const preset = PRESETS.find((p) => p.id === "puzzle-choice");
  const at = insertionPoint(g, after ?? null);
  if (!preset || !at) return null;
  const choice = nodeFromPreset(g, preset, { x: 0, y: 0 });
  const [a, b] = choice.puzzle?.outputs ?? [];
  if (!a || !b) return null;
  const answer = (o: typeof a, value: string) => ({ ...o, label: value, matchers: [{ ...(o.matchers?.[0] ?? {}), kind: "exact" as const, value }] });
  choice.title = words.title;
  choice.puzzle = { ...choice.puzzle, outputs: [answer(a, words.left), answer(b, words.right)] };
  let out = insertAfter(g, at, choice);
  const placed = out.nodes.find((n) => n.slug === choice.slug)!;
  const back: Node = {
    ...newNode(out, "story", { x: placed.x ?? 0, y: (placed.y ?? 0) + 240 }),
    title: words.back,
    story: { blocks: [{ id: "b1", kind: "text", text: words.backText }] },
  };
  out = { ...out, nodes: [...out.nodes, back] };
  out = connect(connect(out, choice.slug, b.port, back.slug), back.slug, "out", choice.slug);
  return { graph: out, slug: choice.slug };
}
