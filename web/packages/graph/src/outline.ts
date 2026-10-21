// An adventure as an outline: every step once, in the order a team meets
// it, with branches indented under the step they leave from. The list
// view draws it — an overview that works on a phone, where a canvas does
// not, and for any shape of adventure, not only a straight line.

import { PORT_DONE, type Graph, type Node } from "./types.ts";
import { nodeBySlug, portLabel, portsOf, targetOf } from "./model.ts";

export type OutlineRow =
  /** A step, at its place in the flow. `via` names the route into a branch. */
  | { kind: "step"; slug: string; depth: number; via?: string }
  /** A route to a step listed elsewhere — a loop back, or a merge. `from` is the step it leaves. */
  | { kind: "jump"; slug: string; depth: number; via?: string; from?: string }
  /**
   * Where a branch ends without a row of its own: it returns to its hub,
   * reaches the step where branches meet, or trails off (`to` unset).
   * Nothing to show, but a place to put a step — see reorder.ts.
   */
  | { kind: "tail"; from: string; to?: string; depth: number }
  /** Heading before the steps nothing leads to. */
  | { kind: "loose" };

/**
 * The outline of a graph. From each start it follows the flow: a step
 * with one way on continues at the same depth; one with several opens a
 * branch for each, indented, and the flow carries on where they meet
 * again. A hub's branches end where they return to it, and its "done"
 * continues after them. A step reached a second time is a jump, not a
 * repeat, so loops and merges stay finite and every step appears once.
 */
export function outline(g: Graph): OutlineRow[] {
  const rows: OutlineRow[] = [];
  const listed = new Set<string>();

  // `from` is the step the walk arrives from, when there is just one.
  const walk = (first: string | undefined, depth: number, stop: ReadonlySet<string>, via?: string, from?: string) => {
    let cur = first;
    let label = via;
    let prev = from;
    while (cur !== undefined) {
      if (stop.has(cur) || listed.has(cur)) {
        // Meeting the end of a branch is silent; a route that goes
        // straight there, or anywhere already listed, is shown as a jump.
        if (label !== undefined || !stop.has(cur)) rows.push({ kind: "jump", slug: cur, depth, ...(label ? { via: label } : {}), ...(prev ? { from: prev } : {}) });
        else if (prev) rows.push({ kind: "tail", from: prev, to: cur, depth });
        return;
      }
      const n = nodeBySlug(g, cur);
      if (!n) return;
      listed.add(cur);
      rows.push({ kind: "step", slug: cur, depth, ...(label ? { via: label } : {}) });
      label = undefined;

      const routes = routesOf(g, n);
      if (routes.length === 0) {
        // A step whose one way on leads nowhere yet.
        if (portsOf(n).length === 1) rows.push({ kind: "tail", from: n.slug, depth });
        return;
      }
      prev = n.slug;
      if (routes.length === 1) {
        cur = routes[0]!.to;
        continue;
      }
      if (n.type === "hub") {
        const done = targetOf(g, n.slug, PORT_DONE);
        const inner = new Set([...stop, n.slug, ...(done ? [done] : [])]);
        for (const r of routes) if (!r.ports.includes(PORT_DONE)) walk(r.to, depth + 1, inner, r.label, n.slug);
        cur = done;
        continue;
      }
      const join = joinPoint(g, n.slug, routes.map((r) => r.to));
      const inner = join ? new Set([...stop, join]) : stop;
      for (const r of routes) walk(r.to, depth + 1, inner, r.label, n.slug);
      // Several branches lead into the join: no one step to arrive from.
      prev = undefined;
      cur = join;
    }
  };

  for (const s of g.nodes.filter((n) => n.type === "start")) walk(s.slug, 0, new Set());

  // What no start reaches: chains that begin nowhere first, so each is
  // listed from its head, then anything left (a loop on its own).
  const loose = g.nodes.filter((n) => !listed.has(n.slug));
  if (loose.length) {
    rows.push({ kind: "loose" });
    const fed = new Set(g.edges.map((e) => e.to));
    for (const n of loose.filter((x) => !fed.has(x.slug))) walk(n.slug, 0, new Set());
    for (const n of loose) if (!listed.has(n.slug)) walk(n.slug, 0, new Set());
  }
  return rows;
}

/** A node's distinct next steps, each with the names of the ports leading there. */
function routesOf(g: Graph, n: Node): { to: string; ports: string[]; label: string }[] {
  const by = new Map<string, string[]>();
  for (const p of portsOf(n)) {
    const to = targetOf(g, n.slug, p);
    if (to !== undefined) by.set(to, [...(by.get(to) ?? []), p]);
  }
  return [...by].map(([to, ports]) => ({ to, ports, label: ports.map((p) => portLabel(n, p, g.roles)).join(" / ") }));
}

/**
 * Where branches leaving `from` meet again: the node every branch
 * reaches, nearest to the farthest of them. Undefined when they never
 * meet. `from` itself is not passed through, so a branch that loops back
 * does not make the branch point its own join.
 */
function joinPoint(g: Graph, from: string, starts: string[]): string | undefined {
  const next = new Map<string, string[]>();
  for (const e of g.edges) next.set(e.from, [...(next.get(e.from) ?? []), e.to]);
  const dist = starts.map((s) => {
    const d = new Map<string, number>([[s, 0]]);
    const queue = [s];
    while (queue.length) {
      const cur = queue.shift()!;
      for (const to of next.get(cur) ?? []) {
        if (to === from || d.has(to)) continue;
        d.set(to, d.get(cur)! + 1);
        queue.push(to);
      }
    }
    return d;
  });
  let best: string | undefined;
  let bestCost = Infinity;
  for (const [slug] of dist[0]!) {
    if (!dist.every((d) => d.has(slug))) continue;
    const cost = Math.max(...dist.map((d) => d.get(slug)!));
    if (cost < bestCost) [best, bestCost] = [slug, cost];
  }
  return best;
}
