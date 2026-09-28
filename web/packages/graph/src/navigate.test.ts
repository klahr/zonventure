import { test } from "node:test";
import assert from "node:assert/strict";
import { neighbour } from "./navigate.ts";
import type { Edge, Graph, Node } from "./types.ts";

const e = (from: string, from_port: string, to: string): Edge => ({ from, from_port, to });
const at = (n: Node, x: number, y: number): Node => ({ ...n, x, y });
const story = (slug: string): Node => ({ slug, type: "story", title: slug });

// start → a → gate ─pass→ b (above) ─→ end
//                  └fail→ c (below) ─→ end
const g: Graph = {
  nodes: [
    at({ slug: "start", type: "start" }, 0, 0),
    at(story("a"), 300, 0),
    at({ slug: "gate", type: "gate", gate: { condition: { op: "" } } }, 600, 0),
    at(story("b"), 900, -100),
    at(story("c"), 900, 100),
    at({ slug: "end", type: "end" }, 1200, 0),
  ],
  edges: [e("start", "out", "a"), e("a", "out", "gate"), e("gate", "pass", "b"), e("gate", "fail", "c"), e("b", "out", "end"), e("c", "out", "end")],
};

test("nothing selected goes to the start", () => {
  assert.equal(neighbour(g, null, "next"), "start");
  assert.equal(neighbour(g, "gone", "down"), "start");
});

test("next follows the first connected output, prev goes back", () => {
  assert.equal(neighbour(g, "start", "next"), "a");
  assert.equal(neighbour(g, "gate", "next"), "b");
  assert.equal(neighbour(g, "a", "prev"), "start");
  assert.equal(neighbour(g, "start", "prev"), undefined);
  assert.equal(neighbour(g, "end", "next"), undefined);
});

test("prev from a join picks the step level with it", () => {
  const moved: Graph = { ...g, nodes: g.nodes.map((n) => (n.slug === "end" ? { ...n, y: 90 } : n)) };
  assert.equal(neighbour(moved, "end", "prev"), "c");
});

test("down and up move between branches of the same step", () => {
  assert.equal(neighbour(g, "b", "down"), "c");
  assert.equal(neighbour(g, "c", "up"), "b");
  assert.equal(neighbour(g, "c", "down"), undefined);
  assert.equal(neighbour(g, "a", "down"), undefined);
});

