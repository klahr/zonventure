import { test } from "node:test";
import assert from "node:assert/strict";
import { mergeGraphs, type Graph } from "./index.ts";

const base: Graph = {
  nodes: [
    { slug: "start", type: "start" },
    { slug: "a", type: "story", title: "A", x: 0, y: 0 },
    { slug: "b", type: "story", title: "B" },
  ],
  edges: [{ from: "start", from_port: "out", to: "a" }],
};

const withNode = (g: Graph, slug: string, patch: object): Graph => ({
  ...g,
  nodes: g.nodes.map((n) => (n.slug === slug ? { ...n, ...patch } : n)),
});

test("edits to different steps both survive", () => {
  const mine = withNode(base, "a", { title: "Mine" });
  const theirs = withNode(base, "b", { title: "Theirs" });
  const r = mergeGraphs(base, mine, theirs);
  assert.deepEqual(r.conflicts, []);
  assert.equal(r.graph.nodes.find((n) => n.slug === "a")?.title, "Mine");
  assert.equal(r.graph.nodes.find((n) => n.slug === "b")?.title, "Theirs");
});

test("different fields of one step merge; moving is never a clash", () => {
  const mine = withNode(base, "a", { title: "Mine", x: 10 });
  const theirs = withNode(base, "a", { x: 99, story: { text: "t" } });
  const r = mergeGraphs(base, mine, theirs);
  assert.deepEqual(r.conflicts, []);
  const a = r.graph.nodes.find((n) => n.slug === "a")!;
  assert.equal(a.title, "Mine");
  assert.equal(a.x, 10);
  assert.deepEqual(a.story, { text: "t" });
});

test("the same field changed twice keeps mine and reports the step", () => {
  const r = mergeGraphs(base, withNode(base, "a", { title: "Mine" }), withNode(base, "a", { title: "Theirs" }));
  assert.deepEqual(r.conflicts, ["a"]);
  assert.equal(r.graph.nodes.find((n) => n.slug === "a")?.title, "Mine");
});

test("additions on both sides keep theirs' order, then mine", () => {
  const mine: Graph = { ...base, nodes: [...base.nodes, { slug: "m", type: "end" }] };
  const theirs: Graph = { ...base, nodes: [...base.nodes, { slug: "t", type: "end" }] };
  const r = mergeGraphs(base, mine, theirs);
  assert.deepEqual(r.graph.nodes.map((n) => n.slug), ["start", "a", "b", "t", "m"]);
});

test("their deletion takes its wires along", () => {
  const theirs: Graph = { nodes: base.nodes.filter((n) => n.slug !== "a"), edges: [] };
  const mine: Graph = { ...base, edges: [...base.edges, { from: "b", from_port: "out", to: "a" }] };
  const r = mergeGraphs(base, mine, theirs);
  assert.ok(!r.graph.nodes.some((n) => n.slug === "a"));
  assert.deepEqual(r.graph.edges, []);
});

test("rewiring one port both ways is a clash on the step it leaves", () => {
  const mine = { ...base, edges: [{ from: "start", from_port: "out", to: "b" }] };
  const theirs = { ...base, edges: [] };
  const r = mergeGraphs(base, mine, theirs);
  assert.deepEqual(r.conflicts, ["start"]);
});

test("zones and translations merge by key", () => {
  const b: Graph = { ...base, zones: [{ slug: "z", lat: 1, lng: 1, radius_m: 5 }], translations: { sv: { k1: "x" } } };
  const mine: Graph = { ...b, translations: { sv: { k1: "x", k2: "mine" } } };
  const theirs: Graph = { ...b, zones: [...b.zones!, { slug: "y", lat: 2, lng: 2, radius_m: 5 }] };
  const r = mergeGraphs(b, mine, theirs);
  assert.deepEqual(r.conflicts, []);
  assert.deepEqual(r.graph.zones?.map((z) => z.slug), ["z", "y"]);
  assert.deepEqual(r.graph.translations, { sv: { k1: "x", k2: "mine" } });
});
