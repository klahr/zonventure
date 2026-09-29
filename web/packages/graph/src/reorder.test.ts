import { test } from "node:test";
import assert from "node:assert/strict";
import { outline } from "./outline.ts";
import { canMove, gapsOf, moveBy, moveInto } from "./reorder.ts";
import { moveStep } from "./model.ts";
import type { Edge, Graph, Node } from "./types.ts";

const e = (from: string, from_port: string, to: string): Edge => ({ from, from_port, to });
const puzzle = (slug: string, ...ports: string[]): Node => ({
  slug,
  type: "puzzle",
  title: slug,
  puzzle: { outputs: ports.map((p) => ({ port: p, label: p, trigger: "answer" as const })) },
});
const story = (slug: string): Node => ({ slug, type: "story", title: slug });
const start: Node = { slug: "start", type: "start" };
const end: Node = { slug: "end", type: "end" };

/** Edges as sorted "from.port>to" strings, so order does not matter. */
const wires = (g: Graph) => g.edges.map((x) => `${x.from}.${x.from_port}>${x.to}`).sort();
/** The outline's steps, indented, as the list view shows them. */
const steps = (g: Graph) =>
  outline(g).flatMap((r) => (r.kind === "step" ? [`${"  ".repeat(r.depth)}${r.slug}`] : r.kind === "jump" ? [`${"  ".repeat(r.depth)}→ ${r.slug}`] : []));
/** The gap above a row, found by the row's step slug or, for a tail, by `tail:<from>`. */
const gapAt = (g: Graph, key: string) => {
  const rows = outline(g);
  const i = rows.findIndex((r) => (r.kind === "step" ? r.slug === key : r.kind === "tail" ? `tail:${r.from}` === key : r.kind === "jump" ? `jump:${r.from}>${r.slug}` === key : false));
  const gap = gapsOf(g, rows).find((x) => x.row === i);
  assert.ok(gap, `no gap at ${key}`);
  return gap;
};

const line = (): Graph => ({
  nodes: [start, story("a"), story("b"), story("c"), end],
  edges: [e("start", "out", "a"), e("a", "out", "b"), e("b", "out", "c"), e("c", "out", "end")],
});

test("on a straight line, up and down match the old swap", () => {
  const g = line();
  assert.deepEqual(wires(moveBy(g, outline(g), "b", -1)!), wires(moveStep(g, "b", -1)));
  assert.deepEqual(wires(moveBy(g, outline(g), "b", 1)!), wires(moveStep(g, "b", 1)));
  assert.equal(moveBy(g, outline(g), "a", -1), null);
  assert.equal(moveBy(g, outline(g), "c", 1), null);
});

test("dropping a step where it already is changes nothing", () => {
  const g = line();
  assert.equal(moveInto(g, "b", gapAt(g, "b")), g);
  assert.equal(moveInto(g, "b", gapAt(g, "c")), g);
});

// start → q ─solved→ well-done ─┐
//            └pity→ hint ───────┴→ next → end
const branched = (): Graph => ({
  nodes: [start, story("pre"), puzzle("q", "solved", "pity"), story("well-done"), story("hint"), story("next"), end],
  edges: [
    e("start", "out", "pre"),
    e("pre", "out", "q"),
    e("q", "solved", "well-done"),
    e("q", "pity", "hint"),
    e("well-done", "out", "next"),
    e("hint", "out", "next"),
    e("next", "out", "end"),
  ],
});

test("a step where the flow splits has no handle", () => {
  const g = branched();
  assert.equal(canMove(g, "q"), false);
  assert.equal(canMove(g, "start"), false);
  assert.equal(canMove(g, "end"), false);
  assert.equal(canMove(g, "hint"), true);
});

test("a step moves from the main line into a branch", () => {
  const g = branched();
  const out = moveInto(g, "pre", gapAt(g, "hint"));
  assert.deepEqual(wires(out), ["hint.out>next", "next.out>end", "pre.out>hint", "q.pity>pre", "q.solved>well-done", "start.out>q", "well-done.out>next"]);
});

test("the gap above where branches meet puts a step after all of them", () => {
  const g = branched();
  const out = moveInto(g, "pre", gapAt(g, "next"));
  assert.deepEqual(steps(out), ["start", "q", "  well-done", "  hint", "pre", "next", "end"]);
  assert.ok(wires(out).includes("well-done.out>pre") && wires(out).includes("hint.out>pre"));
});

test("a branch's tail puts a step at its end, not after the join", () => {
  const g = branched();
  const out = moveInto(g, "pre", gapAt(g, "tail:well-done"));
  assert.deepEqual(steps(out), ["start", "q", "  well-done", "  pre", "  hint", "next", "end"]);
  assert.ok(wires(out).includes("hint.out>next"));
});

test("down walks a step through every gap in reading order", () => {
  const labelled = (g: Graph) =>
    outline(g).flatMap((r) => (r.kind === "step" ? [`${"  ".repeat(r.depth)}${r.via ? `[${r.via}] ` : ""}${r.slug}`] : []));
  let g = branched();
  const seen: string[][] = [];
  for (let out = moveBy(g, outline(g), "pre", 1); out; out = moveBy(g, outline(g), "pre", 1)) {
    g = out;
    seen.push(labelled(g));
  }
  assert.deepEqual(seen, [
    ["start", "q", "  [solved] pre", "  well-done", "  [pity] hint", "next", "end"],
    ["start", "q", "  [solved] well-done", "  pre", "  [pity] hint", "next", "end"],
    ["start", "q", "  [solved] well-done", "  [pity] pre", "  hint", "next", "end"],
    ["start", "q", "  [solved] well-done", "  [pity] hint", "  pre", "next", "end"],
    ["start", "q", "  [solved] well-done", "  [pity] hint", "pre", "next", "end"],
    ["start", "q", "  [solved] well-done", "  [pity] hint", "next", "pre", "end"],
  ]);
});

test("a hub's branches: a step moves into another branch and back out", () => {
  const g: Graph = {
    nodes: [
      start,
      { slug: "home", type: "hub", hub: { policy: "choice", home: true, branches: [{ port: "carl", label: "Carl" }, { port: "dana", label: "Dana" }] } },
      story("carl-arrive"),
      puzzle("carl-q", "solved"),
      story("dana-q"),
      end,
    ],
    edges: [
      e("start", "out", "home"),
      e("home", "carl", "carl-arrive"),
      e("carl-arrive", "out", "carl-q"),
      e("carl-q", "solved", "home"),
      e("home", "dana", "dana-q"),
      e("dana-q", "out", "home"),
    ],
  };
  const out = moveInto(g, "carl-arrive", gapAt(g, "tail:dana-q"));
  assert.deepEqual(wires(out), ["carl-arrive.out>home", "carl-q.solved>home", "dana-q.out>carl-arrive", "home.carl>carl-q", "home.dana>dana-q", "start.out>home"]);
});

test("a loose step is dropped into the flow", () => {
  const g: Graph = { ...line(), nodes: [...line().nodes, story("stray")] };
  const out = moveInto(g, "stray", gapAt(g, "b"));
  assert.deepEqual(steps(out), ["start", "a", "stray", "b", "c", "end"]);
});

test("a step in a two-step loop is lifted without leading to itself", () => {
  // start → a ⇄ b, a's way on to b; b leads back to a.
  const g: Graph = {
    nodes: [start, story("a"), story("b"), story("c"), end],
    edges: [e("start", "out", "a"), e("a", "out", "b"), e("b", "out", "a"), e("c", "out", "end")],
  };
  const out = moveInto(g, "b", { row: -1, depth: 0, slots: [{ from: "c", port: "out" }] });
  assert.ok(!out.edges.some((x) => x.from === x.to));
  assert.deepEqual(wires(out), ["b.out>end", "c.out>b", "start.out>a"]);
});

test("a moved step goes a column right of what it now follows", () => {
  const g = line();
  g.nodes = g.nodes.map((n, i) => ({ ...n, x: i * 336, y: 0 }));
  const out = moveInto(g, "c", gapAt(g, "a"));
  const x = (s: string) => out.nodes.find((n) => n.slug === s)!.x;
  assert.equal(x("c"), x("start")! + 336);
  assert.ok(x("a")! > x("c")!);
  assert.deepEqual(out.nodes.map((n) => n.slug), g.nodes.map((n) => n.slug));
});
