import { test } from "node:test";
import assert from "node:assert/strict";
import { diffGraphs, diffIsEmpty, textChanges, wordDiff, type Graph } from "./index.ts";

const base: Graph = {
  nodes: [
    { slug: "start", type: "start" },
    { slug: "q", type: "puzzle", title: "Q", puzzle: { blocks: [{ id: "b1", kind: "text", text: "?" }] } },
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
      { slug: "q", type: "puzzle", title: "Question", puzzle: { blocks: [{ id: "b1", kind: "text", text: "Why?" }] } },
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
  assert.deepEqual(d.changed, [
    {
      slug: "q",
      title: "Question",
      fields: ["title", "content"],
      texts: [
        { key: "n:q:title", label: "Title", before: "Q", after: "Question" },
        { key: "n:q:block:b1", label: "Text", before: "?", after: "Why?" },
      ],
    },
  ]);
  assert.equal(d.addedEdges.length, 2);
  assert.equal(d.removedEdges.length, 1);
  assert.deepEqual(d.resources, ["zones"]);
});

test("a step's words and its other settings are told apart", () => {
  const withPuzzle = (puzzle: NonNullable<Graph["nodes"][number]["puzzle"]>): Graph => ({ ...base, nodes: base.nodes.map((n) => (n.slug === "q" ? { ...n, puzzle } : n)) });
  const words = { blocks: [{ id: "b1", kind: "text" as const, text: "?" }] };
  assert.deepEqual(diffGraphs(withPuzzle(words), withPuzzle({ ...words, lockout_seconds: 30 })).changed[0]?.fields, ["settings"]);
  assert.deepEqual(diffGraphs(withPuzzle(words), withPuzzle({ blocks: [{ id: "b1", kind: "text", text: "!" }] })).changed[0]?.fields, ["content"]);
});

test("a split's paths and text are its settings", () => {
  const withSplit = (split: NonNullable<Graph["nodes"][number]["split"]>): Graph => ({ ...base, nodes: [...base.nodes, { slug: "sp", type: "split", split }] });
  const paths = [{ port: "a", label: "North" }, { port: "b" }];
  const was = withSplit({ paths });
  for (const next of [
    withSplit({ paths: [{ port: "a", label: "South" }, { port: "b" }] }),
    withSplit({ paths: [{ port: "a", label: "North" }, { port: "b", role: "scout" }] }),
    withSplit({ paths, text: "Go with your group" }),
  ]) {
    const d = diffGraphs(was, next);
    assert.equal(d.changed.length, 1);
    assert.equal(d.changed[0]?.slug, "sp");
    assert.ok(d.changed[0]?.fields.includes("settings"));
  }
});

test("a text that is added or emptied shows the other side empty", () => {
  const q = base.nodes[1]!;
  const changes = textChanges(q, { ...q, puzzle: { blocks: [], hints: [{ text: "Look up" }] } });
  assert.deepEqual(changes, [
    { key: "n:q:hint:0", label: "Hint {{i}}", vars: { i: 1 }, before: "", after: "Look up" },
    { key: "n:q:block:b1", label: "Text", before: "?", after: "" },
  ]);
});

test("a typo fix is one word out and one in", () => {
  assert.deepEqual(wordDiff("The cat sat on teh mat.", "The cat sat on the mat."), [
    { op: "same", text: "The cat sat on " },
    { op: "del", text: "teh" },
    { op: "ins", text: "the" },
    { op: "same", text: " mat." },
  ]);
  assert.deepEqual(wordDiff("", "new"), [{ op: "ins", text: "new" }]);
  assert.deepEqual(wordDiff("same", "same"), [{ op: "same", text: "same" }]);
});
