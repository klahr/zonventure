import { test } from "node:test";
import assert from "node:assert/strict";
import { outline, type OutlineRow } from "./outline.ts";
import type { Edge, Graph, Node } from "./types.ts";

const e = (from: string, from_port: string, to: string): Edge => ({ from, from_port, to });
const puzzle = (slug: string, ...ports: string[]): Node => ({
  slug,
  type: "puzzle",
  title: slug,
  puzzle: { outputs: ports.map((p) => ({ port: p, label: p, trigger: "answer" as const })) },
});
const story = (slug: string): Node => ({ slug, type: "story", title: slug });

/** Rows as short strings: indentation, a jump arrow, the route name. */
const show = (rows: OutlineRow[]) =>
  rows.filter((r) => r.kind !== "tail").map((r) => (r.kind === "loose" ? "-- loose" : `${"  ".repeat(r.depth)}${r.kind === "jump" ? "→ " : ""}${r.via ? `[${r.via}] ` : ""}${r.slug}`));

test("a straight line is one column", () => {
  const g: Graph = {
    nodes: [{ slug: "start", type: "start" }, puzzle("a", "solved"), story("b"), { slug: "end", type: "end" }],
    edges: [e("start", "out", "a"), e("a", "solved", "b"), e("b", "out", "end")],
  };
  assert.deepEqual(show(outline(g)), ["start", "a", "b", "end"]);
});

test("a home hub's branches are indented and end where they return", () => {
  const g: Graph = {
    nodes: [
      { slug: "start", type: "start" },
      { slug: "home", type: "hub", hub: { policy: "choice", home: true, branches: [{ port: "carl", label: "Carl" }, { port: "accuse", label: "Accuse" }] } },
      story("carl-arrive"),
      puzzle("carl-q", "solved"),
      puzzle("accuse", "done"),
      { slug: "end", type: "end" },
    ],
    edges: [
      e("start", "out", "home"),
      e("home", "carl", "carl-arrive"),
      e("carl-arrive", "out", "carl-q"),
      e("carl-q", "solved", "home"),
      e("home", "accuse", "accuse"),
      e("accuse", "done", "end"),
    ],
  };
  assert.deepEqual(show(outline(g)), ["start", "home", "  [Carl] carl-arrive", "  carl-q", "  [Accuse] accuse", "  end"]);
});

test("routes that meet again continue at the step's own depth", () => {
  const g: Graph = {
    nodes: [{ slug: "start", type: "start" }, puzzle("q", "solved", "pity"), story("well-done"), story("hint"), story("next"), { slug: "end", type: "end" }],
    edges: [
      e("start", "out", "q"),
      e("q", "solved", "well-done"),
      e("q", "pity", "hint"),
      e("well-done", "out", "next"),
      e("hint", "out", "next"),
      e("next", "out", "end"),
    ],
  };
  assert.deepEqual(show(outline(g)), ["start", "q", "  [solved] well-done", "  [pity] hint", "next", "end"]);
});

test("a route straight to where the branches meet is a jump", () => {
  const g: Graph = {
    nodes: [{ slug: "start", type: "start" }, puzzle("q", "solved", "skip"), story("extra"), { slug: "end", type: "end" }],
    edges: [e("start", "out", "q"), e("q", "solved", "extra"), e("q", "skip", "end"), e("extra", "out", "end")],
  };
  assert.deepEqual(show(outline(g)), ["start", "q", "  [solved] extra", "  → [skip] end", "end"]);
});

test("a loop back is a jump, and ports to one place share a row", () => {
  const g: Graph = {
    nodes: [{ slug: "start", type: "start" }, puzzle("q", "right", "timeout", "wrong"), story("again"), { slug: "end", type: "end" }],
    edges: [e("start", "out", "q"), e("q", "right", "end"), e("q", "timeout", "end"), e("q", "wrong", "again"), e("again", "out", "q")],
  };
  assert.deepEqual(show(outline(g)), ["start", "q", "  [right / timeout] end", "  [wrong] again", "  → q"]);
});

test("steps nothing leads to are listed last, from the head of their chain", () => {
  const g: Graph = {
    nodes: [{ slug: "start", type: "start" }, story("tail"), story("head"), { slug: "end", type: "end" }],
    edges: [e("start", "out", "end"), e("head", "out", "tail")],
  };
  assert.deepEqual(show(outline(g)), ["start", "end", "-- loose", "head", "tail"]);
});
