import { test } from "node:test";
import assert from "node:assert/strict";
import {
  PRESETS,
  emptyGraph,
  existingTargets,
  nodeFromPreset,
  portsOf,
  presetsFor,
  scorePreset,
  suggestionsFor,
  type Node,
} from "./index.ts";

test("preset ids are unique and every suggestion names a real preset", () => {
  const ids = new Set(PRESETS.map((p) => p.id));
  assert.equal(ids.size, PRESETS.length);
  const froms: Parameters<typeof suggestionsFor>[0][] = [
    null,
    { side: "input", node: { slug: "x", type: "puzzle" } },
    ...(["start", "puzzle", "story", "gate", "effect"] as const).map((type) => ({
      side: "output" as const, node: { slug: "x", type } as Node, port: "out",
    })),
    { side: "output", node: { slug: "h", type: "hub" }, port: "done" },
    { side: "output", node: { slug: "h", type: "hub" }, port: "a" },
  ];
  for (const f of froms) {
    for (const id of suggestionsFor(f)) assert.ok(ids.has(id), `suggestion ${id} is not a preset`);
  }
});

test("every preset builds a node of its declared type, with ports where it needs them", () => {
  const g = emptyGraph();
  for (const p of PRESETS) {
    const n = nodeFromPreset(g, p, { x: 10.6, y: 20.4 });
    assert.equal(n.type, p.type, p.id);
    assert.equal(n.x, 11);
    assert.equal(n.y, 20);
    if (p.type !== "end") assert.ok(portsOf(n).length > 0, `${p.id} has no output`);
  }
});

test("slugs from presets never collide with existing nodes", () => {
  const g = emptyGraph();
  const a = nodeFromPreset(g, PRESETS[0]!, { x: 0, y: 0 });
  const b = nodeFromPreset({ ...g, nodes: [...g.nodes, a] }, PRESETS[0]!, { x: 0, y: 0 });
  assert.notEqual(a.slug, b.slug);
});

// An end has no output, so it cannot be wired into an input the author
// dragged backwards from.
test("a backward drag offers only presets with an output", () => {
  const offered = presetsFor({ side: "input", node: { slug: "x", type: "puzzle" } });
  assert.equal(offered.some((p) => p.type === "end"), false);
  assert.equal(presetsFor(null).length, PRESETS.length);
});

test("search ranks label prefixes first and requires every word", () => {
  const rank = (q: string) =>
    PRESETS.map((p) => ({ id: p.id, s: scorePreset(p, q) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.id);

  assert.equal(rank("gat")[0], "gate");
  assert.equal(rank("crowd")[0], "hub-capacity");
  // Keywords catch the words authors actually use.
  assert.ok(rank("gps").includes("checkin"));
  assert.ok(rank("riddle").includes("puzzle-text"));
  // Every word must match.
  assert.deepEqual(rank("gate zzz"), []);
  assert.equal(scorePreset(PRESETS[0]!, "   "), 1, "an empty query matches everything");
});

test("existing targets exclude the start and the source itself", () => {
  const g = emptyGraph();
  const from = { side: "output" as const, node: g.nodes[0]!, port: "out" };
  const t = existingTargets(g, from);
  assert.deepEqual(t.map((n) => n.slug), ["end"]);
  assert.deepEqual(existingTargets(g, null), []);
});
