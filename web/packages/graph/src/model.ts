// Pure editing operations on a graph. No React, no DOM, so every rule is
// testable with node --test and the canvas can never disagree with the
// list view about what a graph means.
//
// Every function returns a new graph and leaves its input untouched: the
// undo history stores whole snapshots, so a mutation in place would
// silently rewrite history.

import {
  PORT_DONE,
  PORT_FAIL,
  PORT_OUT,
  PORT_PASS,
  type Edge,
  type Graph,
  type Node,
  type NodeType,
} from "./types.ts";

/**
 * A node's output ports in declared order. Mirrors Node.Ports() in
 * internal/graph/graph.go exactly — the editor draws a handle for each,
 * and the validator reports any that is unconnected, so the two must
 * agree on what the ports are.
 */
export function portsOf(n: Node): string[] {
  switch (n.type) {
    case "start":
    case "story":
    case "effect":
      return [PORT_OUT];
    case "gate":
      return [PORT_PASS, PORT_FAIL];
    case "end":
      return [];
    case "puzzle":
      return (n.puzzle?.outputs ?? []).map((o) => o.port);
    case "hub":
      // A hub with no configuration is malformed and has no ports, exactly
      // as in Go; the validator reports it. Offering "done" here would draw
      // a handle the server does not consider to exist.
      if (!n.hub) return [];
      return [...(n.hub.branches ?? []).map((b) => b.port), PORT_DONE];
  }
}

/** Nodes that can be the target of an edge. Start is the entry; nothing leads into it. */
export function hasInput(n: Node): boolean {
  return n.type !== "start";
}

/** A human label for a port, for handles and the list view. */
export function portLabel(n: Node, port: string): string {
  if (n.type === "puzzle") {
    const o = n.puzzle?.outputs?.find((x) => x.port === port);
    return o?.label || port;
  }
  if (n.type === "hub") {
    if (port === PORT_DONE) return "done";
    const b = n.hub?.branches?.find((x) => x.port === port);
    return b?.label || port;
  }
  return port;
}

export function nodeBySlug(g: Graph, slug: string): Node | undefined {
  return g.nodes.find((n) => n.slug === slug);
}

export function targetOf(g: Graph, from: string, port: string): string | undefined {
  return g.edges.find((e) => e.from === from && e.from_port === port)?.to;
}

/**
 * Why a proposed connection is refused, or null if it is fine.
 *
 * The rule is small because the model is: every wire carries the same
 * thing — the team's state — so there is no pin typing to check. What
 * remains is structural, and each refusal matches something the server
 * validator would reject, so the canvas can never offer a wire that makes
 * the graph unsavable.
 */
export function connectionError(g: Graph, from: string, port: string, to: string): string | null {
  const src = nodeBySlug(g, from);
  const dst = nodeBySlug(g, to);
  if (!src || !dst) return "Unknown node.";
  if (!portsOf(src).includes(port)) return `${src.type} has no output "${port}".`;
  if (!hasInput(dst)) return "Nothing can lead back into the start.";
  if (from === to) return "A step cannot lead to itself.";
  return null;
}

/**
 * Connect an output to a node. An output has at most one edge, so this
 * replaces whatever the output led to before — which is what dragging a
 * new wire from an already-connected handle means.
 */
export function connect(g: Graph, from: string, port: string, to: string): Graph {
  if (connectionError(g, from, port, to)) return g;
  const edges = g.edges.filter((e) => !(e.from === from && e.from_port === port));
  edges.push({ from, from_port: port, to });
  return { ...g, edges };
}

export function disconnect(g: Graph, from: string, port: string): Graph {
  return { ...g, edges: g.edges.filter((e) => !(e.from === from && e.from_port === port)) };
}

/**
 * Remove a node and every edge touching it. References to it from
 * conditions are left for the validator to report — silently rewriting
 * someone's condition would be worse than showing them it is broken.
 */
export function removeNode(g: Graph, slug: string): Graph {
  if (nodeBySlug(g, slug)?.type === "start") return g;
  return {
    ...g,
    nodes: g.nodes.filter((n) => n.slug !== slug),
    edges: g.edges.filter((e) => e.from !== slug && e.to !== slug),
  };
}

/** Replace one node, found by slug. */
export function updateNode(g: Graph, slug: string, fn: (n: Node) => Node): Graph {
  let changed = false;
  const nodes = g.nodes.map((n) => {
    if (n.slug !== slug) return n;
    changed = true;
    return fn(n);
  });
  if (!changed) return g;

  // Outputs and branches define ports. If an edit removed a port, its edge
  // has nowhere to hang from; drop it rather than leave a dangling wire.
  const updated = nodes.find((n) => n.slug === slug)!;
  const ports = new Set(portsOf(updated));
  const edges = g.edges.filter((e) => e.from !== slug || ports.has(e.from_port));
  return { ...g, nodes, edges };
}

export function moveNode(g: Graph, slug: string, x: number, y: number): Graph {
  return updateNode(g, slug, (n) => ({ ...n, x: Math.round(x), y: Math.round(y) }));
}

/**
 * A fresh slug. Slugs are the stable address — edges, conditions, saved
 * team history and the MCP tools all refer to a node by slug — so they are
 * generated once and never edited. Titles are what authors rename.
 */
export function freshSlug(g: Graph, type: NodeType): string {
  const taken = new Set(g.nodes.map((n) => n.slug));
  for (let i = 1; ; i++) {
    const s = `${type}-${i}`;
    if (!taken.has(s)) return s;
  }
}

/** A new node of the given type with sensible, valid-shaped defaults. */
export function newNode(g: Graph, type: NodeType, at: { x: number; y: number }): Node {
  const slug = freshSlug(g, type);
  const base = { slug, type, x: Math.round(at.x), y: Math.round(at.y) };
  switch (type) {
    case "puzzle":
      return {
        ...base,
        title: "New puzzle",
        puzzle: {
          prompt: "",
          outputs: [{ port: "solved", label: "Solved", trigger: "answer", matchers: [{ kind: "exact", value: "" }] }],
        },
      };
    case "story":
      return { ...base, title: "New story", story: { text: "" } };
    case "hub":
      return {
        ...base,
        title: "New hub",
        hub: { policy: "choice", branches: [{ port: "a", label: "A" }, { port: "b", label: "B" }] },
      };
    case "gate":
      // An empty condition is "always", so a fresh gate is open rather
      // than a trap an author has to remember to configure.
      return { ...base, title: "New gate", gate: { condition: { op: "" } } };
    case "effect":
      return { ...base, title: "Award points", effect: { kind: "score", points: 10 } };
    case "end":
      return { ...base, title: "End", end: { outcome: "complete" } };
    case "start":
      return { ...base, title: "Start" };
  }
}

export function addNode(g: Graph, node: Node): Graph {
  return { ...g, nodes: [...g.nodes, node] };
}

/** A new adventure: start wired straight to end. Valid, and plays as an immediate finish. */
export function emptyGraph(): Graph {
  return {
    nodes: [
      { slug: "start", type: "start", title: "Start", x: 0, y: 0 },
      { slug: "end", type: "end", title: "End", x: 0, y: 280, end: { outcome: "complete" } },
    ],
    edges: [{ from: "start", from_port: PORT_OUT, to: "end" }],
  };
}

// --- linear projection ---------------------------------------------------

/**
 * The steps of a path-shaped graph, in order, or null if it is not one.
 *
 * Most adventures are a straight line of puzzles, and a canvas is a worse
 * tool for editing a straight line than a list. So the designer shows a
 * list whenever the graph is exactly Start → steps → End with no
 * branching, and only offers the canvas once the author adds a branch.
 *
 * "Path-shaped" means: every step is a puzzle or a story with exactly one
 * output, each output leads to the next step, the last leads to the End,
 * and there is nothing else in the graph.
 */
export function linearSteps(g: Graph): Node[] | null {
  const start = g.nodes.find((n) => n.type === "start");
  const ends = g.nodes.filter((n) => n.type === "end");
  if (!start || ends.length !== 1) return null;

  const steps: Node[] = [];
  const seen = new Set<string>([start.slug]);
  let cur: string | undefined = targetOf(g, start.slug, PORT_OUT);

  while (cur !== undefined) {
    if (seen.has(cur)) return null; // a loop is not a line
    seen.add(cur);
    const n = nodeBySlug(g, cur);
    if (!n) return null;
    if (n.type === "end") break;
    if (n.type !== "puzzle" && n.type !== "story") return null;
    const ports = portsOf(n);
    if (ports.length !== 1) return null;
    steps.push(n);
    cur = targetOf(g, n.slug, ports[0]!);
  }
  if (cur === undefined) return null; // the line trails off without reaching the end

  // Anything not on the line — an orphan, a second branch — means the
  // canvas is needed to show it honestly.
  if (seen.size !== g.nodes.length) return null;
  if (g.edges.length !== steps.length + 1) return null;
  return steps;
}

/**
 * Insert a step at the end of a linear graph, just before the End. Returns
 * the graph unchanged when it is not linear.
 */
export function appendStep(g: Graph, type: "puzzle" | "story"): { graph: Graph; slug?: string } {
  const steps = linearSteps(g);
  if (!steps) return { graph: g };
  const end = g.nodes.find((n) => n.type === "end")!;
  const last = steps.at(-1);
  const lastSlug = last?.slug ?? g.nodes.find((n) => n.type === "start")!.slug;
  const lastPort = last ? portsOf(last)[0]! : PORT_OUT;

  const y = (last?.y ?? 0) + 160;
  const node = newNode(g, type, { x: last?.x ?? 0, y });
  let out = addNode(g, node);
  out = connect(out, lastSlug, lastPort, node.slug);
  out = connect(out, node.slug, portsOf(node)[0]!, end.slug);
  out = moveNode(out, end.slug, end.x ?? 0, y + 160);
  return { graph: out, slug: node.slug };
}

/** Move a step up or down within a linear graph by rewiring around it. */
export function moveStep(g: Graph, slug: string, delta: -1 | 1): Graph {
  const steps = linearSteps(g);
  if (!steps) return g;
  const i = steps.findIndex((s) => s.slug === slug);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= steps.length) return g;
  const order = steps.slice();
  [order[i], order[j]] = [order[j]!, order[i]!];
  return rewireLine(g, order);
}

/** Remove a step from a linear graph, joining its neighbours. */
export function removeStep(g: Graph, slug: string): Graph {
  const steps = linearSteps(g);
  if (!steps) return removeNode(g, slug);
  return rewireLine(removeNode(g, slug), steps.filter((s) => s.slug !== slug));
}

function rewireLine(g: Graph, order: Node[]): Graph {
  const start = g.nodes.find((n) => n.type === "start")!;
  const end = g.nodes.find((n) => n.type === "end")!;
  const edges: Edge[] = [];
  let prev = start.slug;
  let prevPort = PORT_OUT;
  for (const n of order) {
    edges.push({ from: prev, from_port: prevPort, to: n.slug });
    prev = n.slug;
    prevPort = portsOf(n)[0]!;
  }
  edges.push({ from: prev, from_port: prevPort, to: end.slug });
  return { ...g, edges };
}

// --- persistence helpers ---------------------------------------------------

/**
 * Strip what the server would reject or never stores.
 *
 * ParseEnvelope refuses unknown fields, and the React Flow layer tends to
 * leave things like `selected` or `measured` on objects it touches. This
 * builds a clean copy with only the declared fields, so a stray UI
 * property can never turn a save into a 400.
 */
export function cleanGraph(g: Graph): Graph {
  const out: Graph = {
    nodes: g.nodes.map(cleanNode),
    edges: g.edges.map((e) => ({ from: e.from, from_port: e.from_port, to: e.to })),
  };
  if (g.zones?.length) out.zones = g.zones;
  if (g.items?.length) out.items = g.items;
  if (g.counters?.length) out.counters = g.counters;
  if (g.journal?.length) out.journal = g.journal;
  return out;
}

function cleanNode(n: Node): Node {
  const out: Node = { slug: n.slug, type: n.type };
  if (n.title) out.title = n.title;
  if (n.x) out.x = Math.round(n.x);
  if (n.y) out.y = Math.round(n.y);
  if (n.puzzle) out.puzzle = n.puzzle;
  if (n.story) out.story = n.story;
  if (n.hub) out.hub = n.hub;
  if (n.gate) out.gate = n.gate;
  if (n.effect) out.effect = n.effect;
  if (n.end) out.end = n.end;
  return out;
}

/**
 * Whether two graphs differ in anything that would be saved. Drives the
 * "unsaved changes" state. Card positions count: they are part of the
 * saved document, so an author who rearranged the canvas and navigates
 * away should be warned just as if they had edited a prompt.
 */
export function differs(a: Graph, b: Graph): boolean {
  return JSON.stringify(cleanGraph(a)) !== JSON.stringify(cleanGraph(b));
}
