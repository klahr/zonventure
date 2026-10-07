import type { Graph, Node } from "@zonventure/graph";
import { answers, checkInStep, choiceDraft, emptyStep, readOf, welcomeStep, welcomeText } from "./tutorial.ts";

/**
 * The tutorials on a desktop: short guided lessons, each one small thing
 * at a time, with the thing to press in a spotlight and everything else
 * locked. Which step is at hand only moves forward; each step says when
 * it is done, read off the draft and the designer's state, so a step
 * already done some other way is passed by. A lesson only counts what
 * was added since it began, so it works on any adventure.
 */

/** What a step can see of the designer. */
export interface CoachCtx {
  graph: Graph;
  /** The steps and places added since the lesson began; zones are all of them. */
  fresh: Graph;
  /** The places added since the lesson began. */
  newZones: NonNullable<Graph["zones"]>;
  selected: string | null;
  /** The inspector shows the selected step. */
  inspecting: boolean;
  /** The ⋮ menu is open. */
  menu: boolean;
  settings: boolean;
  /** The add-step palette is open. */
  palette: boolean;
  previewing: boolean;
  /** The step the previewed team is on. */
  playing: string | null;
  /** The publish dialog is open. */
  publishing: boolean;
  errors: number;
  /** Things done during the lesson that the draft doesn't show: "look", "publish". */
  flags: string[];
  /** The welcome's text when the lesson began. */
  baseline: string;
}

/** Where to find an element: the first visible match, by text if given. */
export interface Finder {
  sel: string;
  /** The element's own text, or its .field__label's for a form field. */
  text?: string;
  /** A descendant of the match to point at instead. */
  then?: string;
  /** An ancestor of the match to point at instead: a whole card for its button. */
  up?: string;
}

/** What the designer does for a step's button. */
export type CoachAct = "welcome" | "zone" | "checkin" | "puzzle" | "hint" | "choice" | "add-choice";

export interface CoachStep {
  id: string;
  title: string;
  body: string;
  /** Text the author can copy, or have written in by `fill`. */
  example?: string;
  fill?: CoachAct;
  /** A main button that does the step: no spotlight, the card does it. */
  action?: { label: string; act: CoachAct };
  /** The spotlight: the first finder that matches. None → a centred card, all locked. */
  target?: (c: CoachCtx) => Finder[];
  /** Where the arrow points, when not at the spotlight. */
  point?: (c: CoachCtx) => Finder[];
  /** More that may be pressed besides the spotlight. */
  allow?: string[];
  /** Moves on by itself once true. */
  done?: (c: CoachCtx) => boolean;
  /** Moves on with Next, which waits for this. No done and no ready → Next at once. */
  ready?: (c: CoachCtx) => boolean;
  /** Passed by on arrival when already true. */
  skip?: (c: CoachCtx) => boolean;
  /** Moves on once the spotlight is pressed. */
  onClick?: boolean;
}

export const TUTORIAL_IDS = ["create", "places", "puzzles", "choices", "look", "test", "publish"] as const;
export type TutorialId = (typeof TUTORIAL_IDS)[number];

export interface Tutorial {
  id: TutorialId;
  title: string;
  blurb: string;
  steps: CoachStep[];
}

type T = { t: (key: string, vars?: Record<string, string | number>) => string };

const attr = (s: string) => JSON.stringify(s);
const card = (slug: string | undefined): Finder[] => (slug ? [{ sel: `.dz-flow .react-flow__node[data-id=${attr(slug)}]` }] : []);
const SETTINGS = ".modal.modal-wide[role=dialog]";
const PALETTE: Finder = { sel: ".qp" };
const ADD: Finder = { sel: ".editor-add-drop" };

const welcomeDone = (c: CoachCtx) => {
  const w = welcomeText(c.graph);
  return w !== "" && w !== c.baseline.trim();
};
const reads = (n: Node | undefined) => !!n && readOf(n) !== "";
const answered = (n: Node | undefined) => !!n && answers(n).length > 0;
const zoneNamed = (c: CoachCtx) => c.newZones.some((z) => (z.name ?? "").trim() !== "");
/** The check-in being built: from the preset (a zone rule, maybe unset) or done already. */
export const checkInDraft = (g: Graph) => checkInStep(g) ?? g.nodes.find((n) => n.type === "story" && n.story?.zone !== undefined);
/** The question: a puzzle that is not the choice. */
export const questionStep = (g: Graph) => g.nodes.find((n) => n.type === "puzzle" && n.slug !== choiceDraft(g)?.slug);
const hints = (g: Graph) => questionStep(g)?.puzzle?.hints ?? [];
/** Where the choice's second answer leads: the way back. */
const wayBack = (g: Graph) => {
  const c = choiceDraft(g);
  const port = c?.puzzle?.outputs?.[1]?.port;
  return g.edges.find((e) => e.from === c?.slug && e.from_port === port)?.to;
};
/** A step the team can only pass standing in one of the adventure's places. */
const atPlace = (n: Node | undefined) => !!(n?.story?.zone || n?.puzzle?.zone);
const playingOf = (c: CoachCtx) => c.graph.nodes.find((n) => n.slug === c.playing);
/** The step a new one goes after: the latest check-in or puzzle, else the welcome. */
export const lastStep = (g: Graph) => questionStep(g) ?? checkInStep(g) ?? welcomeStep(g);

export function coachTutorials(t: T): Tutorial[] {
  const text: Finder = { sel: `.inspector .blk-ed [aria-label=${attr(t.t("Text"))}]` };
  const drill = (part: string): Finder => ({ sel: `.inspector [aria-label=${attr(t.t("Edit {{part}}", { part: t.t(part) }))}]`, up: ".ipart" });
  const back: Finder = { sel: `.inspector [aria-label=${attr(t.t("Back to all settings"))}]` };
  const more: Finder = { sel: `.editor-toolbar [aria-label=${attr(t.t("More"))}]` };
  const settingsItem: Finder = { sel: ".canvas-context-menu [role=menuitem]", text: t.t("Adventure settings") };
  const close: Finder = { sel: `${SETTINGS} .modal-head [aria-label=${attr(t.t("Close"))}]` };
  /** The inspector's field on the step, or the step's card to open it first. */
  const onStep = (slug: string | undefined, c: CoachCtx, ...f: Finder[]) => (c.selected === slug && c.inspecting ? [...f, ...card(slug)] : card(slug));
  const sel = (c: CoachCtx, n: Node | undefined) => !!n && c.selected === n.slug;
  const openSettings = (id: string, body: string, done: (c: CoachCtx) => boolean): CoachStep[] => [
    { id: id + "-menu", title: t.t("Open the menu"), body, target: () => [more], done: (c) => c.menu || c.settings || done(c) },
    { id: id + "-settings", title: t.t("Open Adventure settings"), body: t.t("Pick Adventure settings."), target: () => [settingsItem, more], done: (c) => c.settings || done(c) },
  ];
  const addAfter = (id: string, body: string, after: (c: CoachCtx) => Node | undefined, done: (c: CoachCtx) => boolean): CoachStep[] => [
    {
      id: id + "-select",
      title: t.t("Pick where the step goes"),
      body,
      target: (c) => card(after(c)?.slug),
      done: (c) => sel(c, after(c)) || c.palette || done(c),
    },
    { id: id + "-add", title: t.t("Add a step"), body: t.t("Press Add step."), target: () => [ADD], done: (c) => c.palette || done(c) },
  ];
  const checkIn = (c: CoachCtx) => checkInDraft(c.fresh);
  const tied = (c: CoachCtx) => !!checkInStep({ ...c.fresh, zones: c.graph.zones ?? [] });
  const question = (c: CoachCtx) => questionStep(c.fresh);
  const choice = (c: CoachCtx) => choiceDraft(c.fresh);

  return [
    {
      id: "create",
      title: t.t("Create an adventure"),
      blurb: t.t("Start Erik's stolen elephants and write its welcome."),
      steps: [
        {
          id: "intro",
          title: t.t("This is the designer"),
          body: t.t("Each card is a step the teams go through, from Start to the end. Follow the pulsing arrow: the rest of the screen stays locked until the step is done. Pause unlocks it whenever you want to look around."),
        },
        {
          id: "welcome-open",
          title: t.t("Open the welcome"),
          body: t.t("This is the first page the teams read. Click it to open it on the right."),
          target: (c) => card(welcomeStep(c.graph)?.slug),
          done: (c) => (sel(c, welcomeStep(c.graph)) && c.inspecting) || welcomeDone(c),
        },
        {
          id: "welcome-write",
          title: t.t("Write the welcome"),
          body: t.t("Under What the team sees, tell the teams who needs help, and why. Write your own, or let the tutorial write it."),
          example: t.t("Hi! I'm Erik, the zookeeper. Last night someone opened the gate and my three elephants **wandered off** into the park.\n\nFollow the clues, and hurry: they get grumpy after dark!"),
          fill: "welcome",
          target: (c) => onStep(welcomeStep(c.graph)?.slug, c, text),
          ready: welcomeDone,
          skip: welcomeDone,
        },
      ],
    },
    {
      id: "places",
      title: t.t("Places and check-ins"),
      blurb: t.t("Mark a place on the map and send the team there."),
      steps: [
        ...openSettings("zone", t.t("Places live in the adventure's settings. Open the ⋮ menu."), zoneNamed),
        {
          id: "zone-add",
          title: t.t("Add a zone"),
          body: t.t("A zone is a place on the map. The team's phones know when they stand inside it. Press Add zone."),
          target: () => [{ sel: `${SETTINGS} button.add-btn`, text: t.t("Add zone") }],
          done: (c) => c.newZones.length > 0,
        },
        {
          id: "zone-name",
          title: t.t("Name the zone"),
          body: t.t("Call it The big oak."),
          example: t.t("The big oak"),
          fill: "zone",
          target: () => [{ sel: `${SETTINGS} .box input[aria-label=${attr(t.t("Name"))}]` }],
          ready: zoneNamed,
          skip: zoneNamed,
        },
        {
          id: "zone-pin",
          title: t.t("Put it on the map"),
          body: t.t("Drag the pin onto a tree near you, or leave it where it is for now. Keep the radius at 30 metres or more: GPS drifts between buildings."),
          target: () => [{ sel: `${SETTINGS} .zone-map` }],
        },
        { id: "zone-close", title: t.t("Close the settings"), body: t.t("That's the place. Close the settings."), target: () => [close], done: (c) => !c.settings },
        ...addAfter("checkin", t.t("A new step goes after the one you have selected. Click the welcome."), (c) => welcomeStep(c.graph), (c) => !!checkIn(c)),
        {
          id: "checkin-pick",
          title: t.t("Send the team there"),
          body: t.t("Type check in and pick Check in at a place. Name it Footprints, then write where the team should go. Press Enter after each."),
          example: t.t("Elephant footprints! They lead to the biggest tree in the park. Go there."),
          target: () => [PALETTE, ADD],
          done: (c) => !c.palette && !!checkIn(c),
        },
        {
          id: "checkin-write",
          title: t.t("Say where to go"),
          body: t.t("Under What the team sees, write where the team should go."),
          example: t.t("Elephant footprints! They lead to the biggest tree in the park. Go there."),
          fill: "checkin",
          target: (c) => onStep(checkIn(c)?.slug, c, text),
          ready: (c) => reads(checkIn(c)),
          skip: (c) => reads(checkIn(c)),
        },
        {
          id: "checkin-zone",
          title: t.t("Tie it to the place"),
          body: t.t("Open Rules and pick The big oak under Only continue inside a zone. The team can only go on once it stands there."),
          target: (c) => onStep(checkIn(c)?.slug, c, { sel: ".inspector .field", text: t.t("Only continue inside a zone"), then: "select" }, drill("Rules")),
          done: tied,
        },
      ],
    },
    {
      id: "puzzles",
      title: t.t("Puzzles and hints"),
      blurb: t.t("Ask a question, accept more than one answer and help a stuck team."),
      steps: [
        ...addAfter("puzzle", t.t("The puzzle comes after the step you select. Click it."), (c) => lastStep(c.graph), (c) => !!question(c)),
        {
          id: "puzzle-pick",
          title: t.t("Ask a question"),
          body: t.t("Pick Puzzle. Call it Count the trunks: players see the name as a heading. Write the question, and give 7 as the answer. Press Enter after each."),
          example: t.t("Erik left a note in the bark: “Count the elephant trunks painted on the sign by the pond.”\n\nHow many trunks are there?"),
          target: () => [PALETTE, ADD],
          done: (c) => !c.palette && !!question(c),
        },
        {
          id: "puzzle-write",
          title: t.t("Write the question"),
          body: t.t("Under What the team sees, write the question."),
          example: t.t("Erik left a note in the bark: “Count the elephant trunks painted on the sign by the pond.”\n\nHow many trunks are there?"),
          fill: "puzzle",
          target: (c) => onStep(question(c)?.slug, c, text),
          ready: (c) => reads(question(c)),
          skip: (c) => reads(question(c)),
        },
        {
          id: "puzzle-answer",
          title: t.t("Give the answer"),
          body: t.t("Open Answers and next steps and type 7 as the answer. Capital letters and extra spaces never matter."),
          target: (c) => onStep(question(c)?.slug, c, { sel: `.inspector input[aria-label=${attr(t.t("Answer"))}]` }, drill("Answers and next steps")),
          ready: (c) => answered(question(c)),
          skip: (c) => answered(question(c)),
        },
        {
          id: "puzzle-also",
          title: t.t("Take seven too"),
          body: t.t("Press Also accept and type seven, so a team that writes the word gets it right too."),
          target: (c) => onStep(question(c)?.slug, c, { sel: ".inspector button.add-btn", text: t.t("Also accept") }, drill("Answers and next steps")),
          allow: [".inspector .box"],
          ready: (c) => (question(c)?.puzzle?.outputs?.[0]?.matchers ?? []).filter((m) => (m.value ?? "").trim() !== "").length > 1,
        },
        {
          id: "hint-add",
          title: t.t("Help a stuck team"),
          body: t.t("Open Hints and press Add hint. A hint unlocks after the time you set, counted from when the team arrived, so a fast team never sees it."),
          target: (c) => onStep(question(c)?.slug, c, { sel: ".inspector button.add-btn", text: t.t("Add hint") }, drill("Hints"), back),
          done: (c) => hints(c.fresh).length > 0,
        },
        {
          id: "hint-write",
          title: t.t("Write the hint"),
          body: t.t("Write the hint."),
          example: t.t("The sign is on the side of the pond that faces the playground."),
          fill: "hint",
          allow: [".inspector .list-ed__item"],
          target: (c) => onStep(question(c)?.slug, c, { sel: `.inspector [aria-label=${attr(t.t("Hint"))}]` }, drill("Hints"), back),
          ready: (c) => hints(c.fresh).some((h) => h.text.trim() !== ""),
          skip: (c) => hints(c.fresh).some((h) => h.text.trim() !== ""),
        },
      ],
    },
    {
      id: "choices",
      title: t.t("Choices"),
      blurb: t.t("Let the team pick a way, and send a wrong turn back."),
      steps: [
        {
          id: "choice-add",
          title: t.t("Let the team choose a way"),
          body: t.t("A choice is a puzzle whose answers lead different ways. This one asks left or right: left goes on to the end, right to a dead end that sends the team back to choose again."),
          action: { label: t.t("Add the choice"), act: "add-choice" },
          done: (c) => !!choice(c),
        },
        {
          id: "choice-open",
          title: t.t("Open the choice"),
          body: t.t("Here it is. Click it."),
          target: (c) => card(choice(c)?.slug),
          done: (c) => sel(c, choice(c)) || reads(choice(c)),
        },
        {
          id: "choice-write",
          title: t.t("Ask left or right"),
          body: t.t("Under What the team sees, describe the two paths."),
          example: t.t("Two paths leave the pond. On the left the grass is flattened, and someone dropped half a banana. On the right the path is neat and tidy.\n\nLeft or right?"),
          fill: "choice",
          target: (c) => onStep(choice(c)?.slug, c, text),
          ready: (c) => reads(choice(c)),
          skip: (c) => reads(choice(c)),
        },
        {
          id: "choice-back",
          title: t.t("The way back"),
          body: t.t("Right leads here: a dead end that sends the team back to the choice. Every way out of a step must lead somewhere."),
          target: (c) => card(wayBack(c.fresh)),
        },
      ],
    },
    {
      id: "look",
      title: t.t("The look"),
      blurb: t.t("Pick a look for the phones and line up the cards."),
      steps: [
        ...openSettings("look", t.t("The look is in the adventure's settings. Open the ⋮ menu."), (c) => c.flags.includes("look")),
        {
          id: "look-pick",
          title: t.t("Pick a look"),
          body: t.t("Under Look, pick another Main look: the adventure changes at once."),
          target: () => [{ sel: `${SETTINGS} .look .field`, text: t.t("Main look"), then: "select" }, { sel: `${SETTINGS} .look` }],
          done: (c) => c.flags.includes("look"),
        },
        { id: "look-close", title: t.t("Close the settings"), body: t.t("Close the settings."), target: () => [close], done: (c) => !c.settings },
        { id: "look-tidy", title: t.t("Line it up"), body: t.t("Press Tidy to line the cards up."), target: () => [{ sel: "[data-tidy=toolbar]" }], onClick: true },
      ],
    },
    {
      id: "test",
      title: t.t("Test it"),
      blurb: t.t("Fix what's missing, then play it like a team."),
      steps: [
        {
          id: "test-fix",
          title: t.t("Tie up the loose ends"),
          body: t.t("Every way out of a step must lead somewhere, and every page needs something to read. Press the red count to see what is left, and fill in any step that is still empty."),
          target: (c) => [{ sel: ".editor-issues-pop" }, { sel: ".editor-issues-btn[data-kind=error]" }, ...card(emptyStep(c.graph)?.slug)],
          allow: [".editor-issues-btn", ".editor-issues-pop", ".inspector", ".dz-flow"],
          done: (c) => c.errors === 0 && !emptyStep(c.graph),
        },
        { id: "test-run", title: t.t("Play it yourself"), body: t.t("Press Test play."), target: () => [{ sel: ".editor-run" }], done: (c) => c.previewing },
        {
          id: "test-walk",
          title: t.t("Play it like a team"),
          body: t.t("The phone is the adventure as a team sees it. Press Continue on it to go on, step by step."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}]` }],
          point: () => [{ sel: ".float-win .sim .phone .pv-dock" }, { sel: ".float-win .sim .phone" }],
          done: (c) => atPlace(playingOf(c)) || playingOf(c)?.type === "puzzle",
        },
        {
          id: "test-place",
          title: t.t("Go to the place"),
          body: t.t("This step only lets the team on once it stands at the place. Under Team is at, pick that place: the phone says the team is there, and it can go on."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}]` }],
          point: () => [{ sel: ".float-win .sim__where" }],
          skip: (c) => !atPlace(playingOf(c)),
          done: (c) => !atPlace(playingOf(c)),
        },
        {
          id: "test-on",
          title: t.t("Walk on to a puzzle"),
          body: t.t("Press Continue until the team reaches a puzzle."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}]` }],
          point: () => [{ sel: ".float-win .sim .phone .pv-dock" }, { sel: ".float-win .sim .phone" }],
          skip: (c) => playingOf(c)?.type === "puzzle" || !c.graph.nodes.some((n) => n.type === "puzzle"),
          done: (c) => playingOf(c)?.type === "puzzle",
        },
        {
          id: "test-wrong",
          title: t.t("Answer it wrong"),
          body: t.t("Type a wrong answer on the phone and send it: this is what a team sees when it's wrong."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}]` }],
          point: () => [{ sel: ".float-win .sim .phone .pv-dock" }, { sel: ".float-win .sim .phone" }],
          skip: (c) => playingOf(c)?.type !== "puzzle",
        },
        {
          id: "test-hint",
          title: t.t("Let time pass"),
          body: t.t("Press +5 min: the clock jumps ahead for the team. Then tap Hint on the phone: the hint that was locked is ready to take."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}]` }],
          point: () => [{ sel: ".float-win .sim__controls button", text: "+5 " + t.t("min") }],
          skip: (c) => playingOf(c)?.type !== "puzzle",
        },
        {
          id: "test-close",
          title: t.t("Close Test play"),
          body: t.t("Close the preview."),
          target: () => [{ sel: `.float-win[aria-label=${attr(t.t("Preview"))}] .float-win__bar [aria-label=${attr(t.t("Close"))}]` }],
          done: (c) => !c.previewing,
        },
      ],
    },
    {
      id: "publish",
      title: t.t("Publish"),
      blurb: t.t("Make it playable in events."),
      steps: [
        {
          id: "publish",
          title: t.t("Publish it"),
          body: t.t("Press Publish. Events that start from now on play this version; what you change afterwards stays in the draft until you publish again."),
          target: () => [{ sel: ".editor-publish" }],
          done: (c) => c.publishing || c.flags.includes("publish"),
        },
        {
          id: "publish-confirm",
          title: t.t("Publish it"),
          body: t.t("Press Publish once more."),
          target: () => [{ sel: ".modal.confirm-dialog" }, { sel: ".editor-publish" }],
          point: () => [{ sel: ".modal.confirm-dialog .modal-foot button.primary" }],
          done: (c) => c.flags.includes("publish"),
        },
      ],
    },
  ];
}

/**
 * The step at hand: from `at`, passed by while done, or skipped on
 * arrival. Never goes back by itself, so a closed dialog doesn't undo a
 * step; `held` keeps a step the author went back to, done or not.
 * `steps.length` when the lesson is over.
 */
export function coachAt(steps: CoachStep[], at: string | undefined, c: CoachCtx, held = false): number {
  if (at === DONE) return steps.length;
  if (held) return Math.max(0, steps.findIndex((s) => s.id === at));
  let i = Math.max(0, steps.findIndex((s) => s.id === at));
  while (i < steps.length && (steps[i]!.done?.(c) || (steps[i]!.id !== at && steps[i]!.skip?.(c)))) i++;
  return i;
}

/** `at` once a lesson's last step is behind it. */
export const DONE = "done";

/** A lesson under way: where it is, and what the adventure had when it began. */
export interface CoachRun {
  id: TutorialId;
  at?: string;
  baseline: string;
  before: { nodes: string[]; zones: string[] };
  flags: string[];
  /** Gone back to: stays until Next, though done already. */
  held?: boolean;
  /** Paused: the designer is the author's again until they resume. */
  paused?: boolean;
}

/** Per account, in this browser: a convenience, never state that matters. */
export interface CoachProgress {
  done: TutorialId[];
  run?: CoachRun | undefined;
  /** The list of lessons: open, a pill, or out of sight. */
  panel: "open" | "min" | "closed";
}

const key = (user: number) => "zv_tutorials:" + user;

export function loadCoach(user: number): CoachProgress {
  try {
    const p = JSON.parse(localStorage.getItem(key(user)) ?? "null") as CoachProgress | null;
    if (p && Array.isArray(p.done)) return p;
  } catch {
    /* blocked: start afresh */
  }
  return { done: [], panel: "closed" };
}

export function saveCoach(user: number, p: CoachProgress) {
  try {
    localStorage.setItem(key(user), JSON.stringify(p));
  } catch {
    /* private mode: progress lasts this visit */
  }
}

export function startRun(id: TutorialId, g: Graph, baseline = welcomeText(g)): CoachRun {
  return { id, baseline, before: { nodes: g.nodes.map((n) => n.slug), zones: (g.zones ?? []).map((z) => z.slug) }, flags: [] };
}

/** What a lesson sees as its own: the steps added since it began, with every place. */
export function freshOf(g: Graph, run: Pick<CoachRun, "before">): { fresh: Graph; newZones: NonNullable<Graph["zones"]> } {
  const nodes = new Set(run.before.nodes);
  const zones = new Set(run.before.zones);
  return {
    fresh: { ...g, nodes: g.nodes.filter((n) => !nodes.has(n.slug)) },
    newZones: (g.zones ?? []).filter((z) => !zones.has(z.slug)),
  };
}
