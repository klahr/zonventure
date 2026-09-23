import { test } from "node:test";
import assert from "node:assert/strict";
import { diffGraphs, diffIsEmpty, type Graph } from "./index.ts";

const base: Graph = {
  nodes: [
    { slug: "start", type: "start" },
    { slug: "q", type: "puzzle", title: "Q", puzzle: { prompt: "?" } },
    { slug: "end", type: "end" },
  ],
  edges: [
    { from: "start", from_port: "out", to: "q" },
    { from: "q", from_port: "solved", to: "end" },
  ],
};

test("moving cards is not a change", () => {
  const moved = { ...base, nodes: base.nodes.map((n) => ({ ...n, x: 100, y: 50 })) };
  assert.ok(diffIsEmpty(diffGraphs(base, moved)));
});

test("steps, wires and resources are itemised", () => {
  const draft: Graph = {
    nodes: [
      { slug: "start", type: "start" },
      { slug: "q", type: "puzzle", title: "Question", puzzle: { prompt: "Why?" } },
      { slug: "s", type: "story", title: "Tale" },
      { slug: "end", type: "end" },
    ],
    edges: [
      { from: "start", from_port: "out", to: "q" },
      { from: "q", from_port: "solved", to: "s" },
      { from: "s", from_port: "out", to: "end" },
    ],
    zones: [{ slug: "z", lat: 1, lng: 1, radius_m: 10 }],
  };
  const d = diffGraphs(base, draft);
  assert.deepEqual(d.added, [{ slug: "s", title: "Tale" }]);
  assert.deepEqual(d.changed, [{ slug: "q", title: "Question", fields: ["title", "settings"] }]);
  assert.equal(d.addedEdges.length, 2);
  assert.equal(d.removedEdges.length, 1);
  assert.deepEqual(d.resources, ["zones"]);
});
