// Moving a step within the flow, for the list view: its handles and its
// up and down buttons.
//
// Every gap between two rows of the outline is a connection in the
// graph — the route that leads to the row below. Moving a step lifts it
// out of the flow (whatever led to it now leads to what came after it)
// and drops it into a gap (the connection now runs through it). That is
// the same whether the gap is on the main line, inside a branch or
// between branches, so a step can go anywhere the list shows a gap.
//
// Only a step with one way on can move: a step where the flow splits
// takes its branches with it, which is the canvas's job.

import type { Edge, Graph } from "./types.ts";
import { makeRoom, nodeBySlug, portsOf, STEP_PITCH } from "./model.ts";
import type { OutlineRow } from "./outline.ts";

/** An output a step can be put after. */
export interface Slot {
  from: string;
  port: string;
}

/** A gap in the outline: the outputs that lead to the row it sits above. */
export interface Gap {
  /** Index of that row in the outline. */
  row: number;
  depth: number;
  slots: Slot[];
}

/** Whether a step can be moved on its own: it is not the start or an end, and has one way on. */
export function canMove(g: Graph, slug: string): boolean {
  const n = nodeBySlug(g, slug);
  return !!n && n.type !== "start" && n.type !== "end" && portsOf(n).length === 1;
}

/**
 * The gaps of an outline, in its order. Above a step, the gap is every
 * route into it from a step listed earlier: the one it is reached by,
 * or — where branches meet — the end of each. Above a jump, it is the
 * route the jump stands for; a tail row is the end of its branch. A
 * step nothing earlier leads to (the start, a loose one) has no gap.
 */
export function gapsOf(g: Graph, rows: OutlineRow[]): Gap[] {
  const at = new Map<string, number>();
  rows.forEach((r, i) => {
    if (r.kind === "step") at.set(r.slug, i);
  });
  const routes = (from: string, to: string | undefined): Slot[] =>
    to === undefined
      ? portsOf(nodeBySlug(g, from)!)
          .filter((p) => !g.edges.some((e) => e.from === from && e.from_port === p))
          .map((port) => ({ from, port }))
      : g.edges.filter((e) => e.from === from && e.to === to).map((e) => ({ from, port: e.from_port }));

  const gaps: Gap[] = [];
  rows.forEach((r, i) => {
    let slots: Slot[] = [];
    if (r.kind === "step") {
      slots = g.edges.filter((e) => e.to === r.slug && (at.get(e.from) ?? Infinity) < i).map((e) => ({ from: e.from, port: e.from_port }));
    } else if (r.kind === "jump" && r.from) {
      slots = routes(r.from, r.slug);
    } else if (r.kind === "tail") {
      slots = routes(r.from, r.to);
    }
    if (slots.length && r.kind !== "loose") gaps.push({ row: i, depth: r.depth, slots });
  });
  return gaps;
}

/**
 * Move a step into a gap. Returns the graph unchanged when the step
 * cannot move or the gap is where it already is.
 */
export function moveInto(g: Graph, slug: string, gap: Gap): Graph {
  const n = nodeBySlug(g, slug);
  if (!n || !canMove(g, slug)) return g;
  const port = portsOf(n)[0]!;
  const out = g.edges.find((e) => e.from === slug && e.from_port === port)?.to;
  const into = g.edges.filter((e) => e.to === slug);

  // Lift it out: what led to it leads on to what came after it. A route
  // that would now lead back to its own step is left open instead.
  let edges: Edge[] = g.edges.filter((e) => e.from !== slug && e.to !== slug);
  if (out !== undefined) for (const e of into) if (e.from !== out) edges.push({ ...e, to: out });

  // The gap just below the step is, once it is lifted, the one above it.
  const slots = gap.slots.flatMap((s) => (s.from === slug ? into.map((e) => ({ from: e.from, port: e.from_port })) : [s]));
  if (!slots.length) return g;
  const isSlot = (e: Edge) => slots.some((s) => s.from === e.from && s.port === e.from_port);
  const next = edges.find(isSlot)?.to;

  // Drop it in: the gap's routes lead to it, and it leads on.
  edges = edges.filter((e) => !isSlot(e));
  for (const s of slots) edges.push({ from: s.from, from_port: s.port, to: slug });
  if (next !== undefined) edges.push({ from: slug, from_port: port, to: next });

  if (sameEdges(g.edges, edges)) return g;

  // On the canvas it goes a column right of what it now follows.
  const before = nodeBySlug(g, slots[0]!.from);
  const fx = before?.x ?? 0;
  const room = makeRoom({ ...g, nodes: g.nodes.filter((x) => x.slug !== slug) }, fx);
  const placed = { ...n, x: fx + STEP_PITCH, y: before?.y ?? 0 };
  const moved = new Map(room.nodes.map((x) => [x.slug, x]));
  return { ...room, nodes: g.nodes.map((x) => (x.slug === slug ? placed : moved.get(x.slug)!)), edges };
}

/**
 * Move a step one gap up or down the outline: to the nearest gap that
 * puts it somewhere new. Null when there is none that way.
 */
export function moveBy(g: Graph, rows: OutlineRow[], slug: string, delta: -1 | 1): Graph | null {
  const row = rows.findIndex((r) => r.kind === "step" && r.slug === slug);
  if (row < 0 || !canMove(g, slug)) return null;
  const gaps = gapsOf(g, rows);
  const ahead = delta < 0 ? gaps.filter((x) => x.row <= row).reverse() : gaps.filter((x) => x.row > row);
  for (const gap of ahead) {
    const out = moveInto(g, slug, gap);
    if (out !== g) return out;
  }
  return null;
}

function sameEdges(a: Edge[], b: Edge[]): boolean {
  const key = (e: Edge) => `${e.from}\u0000${e.from_port}\u0000${e.to}`;
  const as = new Set(a.map(key));
  return a.length === b.length && b.every((e) => as.has(key(e)));
}
