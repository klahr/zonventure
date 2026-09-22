import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  appendStep,
  cleanGraph,
  connect,
  connectionError,
  differs,
  disconnect,
  emptyGraph,
  freshSlug,
  linearSteps,
  moveStep,
  newNode,
  portsOf,
  removeNode,
  removeStep,
  tidy,
  updateNode,
  type Graph,
  type Node,
} from "./index.ts";

// The same vectors internal/graph's Go tests read. If portsOf and Go's
// Node.Ports() ever disagree, the editor draws handles the server does
// not think exist — and the validator reports "unconnected port" for a
// handle the author cannot see.
describe("portsOf matches Go's Node.Ports()", () => {
  const vectors = JSON.parse(
    readFileSync(new URL("../testdata/ports.json", import.meta.url), "utf8"),
  ) as { cases: { node: Node; ports: string[] }[] };

  for (const c of vectors.cases) {
    test(`${c.node.type} ${c.node.slug}`, () => {
      assert.deepEqual(portsOf(c.node), c.ports);
    });
  }
});

function linear3(): Graph {
  let g = emptyGraph();
  g = appendStep(g, "puzzle").graph;
  g = appendStep(g, "story").graph;
  g = appendStep(g, "puzzle").graph;
  return g;
}

describe("connections", () => {
  test("refuses what the server would reject", () => {
    const g = linear3();
    assert.match(connectionError(g, "puzzle-1", "solved", "start") ?? "", /start/);
    assert.match(connectionError(g, "puzzle-1", "solved", "puzzle-1") ?? "", /itself/);
    assert.match(connectionError(g, "puzzle-1", "nope", "end") ?? "", /no output/);
    assert.match(connectionError(g, "ghost", "out", "end") ?? "", /Unknown/);
    assert.equal(connectionError(g, "puzzle-1", "solved", "end"), null);
  });

  // An output has at most one edge. Dragging a new wire from a connected
  // handle means "send it here instead".
  test("connecting an output replaces its previous edge", () => {
    let g = linear3();
    g = connect(g, "puzzle-1", "solved", "end");
    const out = g.edges.filter((e) => e.from === "puzzle-1");
    assert.equal(out.length, 1);
    assert.equal(out[0]!.to, "end");
  });

  test("a refused connection leaves the graph untouched", () => {
    const g = linear3();
    assert.equal(connect(g, "puzzle-1", "solved", "start"), g);
  });

  test("disconnect removes just that output's edge", () => {
    const g = disconnect(linear3(), "puzzle-1", "solved");
    assert.equal(g.edges.some((e) => e.from === "puzzle-1"), false);
    assert.equal(g.edges.length, 3);
  });
});

describe("editing", () => {
  test("never mutates its input — history holds snapshots", () => {
    const g = linear3();
    const before = JSON.stringify(g);
    connect(g, "puzzle-1", "solved", "end");
    removeNode(g, "story-1");
    updateNode(g, "puzzle-1", (n) => ({ ...n, title: "changed" }));
    appendStep(g, "puzzle");
    assert.equal(JSON.stringify(g), before);
  });

  test("removing a node removes every edge touching it", () => {
    const g = removeNode(linear3(), "story-1");
    assert.equal(g.nodes.some((n) => n.slug === "story-1"), false);
    assert.equal(g.edges.some((e) => e.from === "story-1" || e.to === "story-1"), false);
  });

  test("the start node cannot be removed", () => {
    const g = linear3();
    assert.equal(removeNode(g, "start"), g);
  });

  // Removing a puzzle output removes its port; its wire has nothing to
  // hang from and must go too, or the saved graph has a dangling edge.
  test("removing an output drops the edge that hung from it", () => {
    let g = emptyGraph();
    const p = newNode(g, "puzzle", { x: 0, y: 0 });
    p.puzzle!.outputs!.push({ port: "left", trigger: "answer", matchers: [{ kind: "exact", value: "l" }] });
    g = { ...g, nodes: [...g.nodes, p] };
    g = connect(g, p.slug, "left", "end");
    g = updateNode(g, p.slug, (n) => ({
      ...n,
      puzzle: { ...n.puzzle!, outputs: n.puzzle!.outputs!.filter((o) => o.port !== "left") },
    }));
    assert.equal(g.edges.some((e) => e.from === p.slug && e.from_port === "left"), false);
  });

  test("fresh slugs never collide", () => {
    let g = emptyGraph();
    const seen = new Set<string>();
    for (let i = 0; i < 20; i++) {
      const s = freshSlug(g, "puzzle");
      assert.equal(seen.has(s), false);
      seen.add(s);
      g = { ...g, nodes: [...g.nodes, newNode(g, "puzzle", { x: 0, y: 0 })] };
    }
  });

  // A freshly dragged gate must be open, not a trap.
  test("a new gate has an empty (always) condition", () => {
    const n = newNode(emptyGraph(), "gate", { x: 0, y: 0 });
    assert.equal(n.gate?.condition.op, "");
  });
});

describe("linear projection", () => {
  test("a straight line is linear, in order", () => {
    const steps = linearSteps(linear3());
    assert.deepEqual(steps?.map((s) => s.slug), ["puzzle-1", "story-1", "puzzle-2"]);
  });

  test("an empty adventure is a line of zero steps", () => {
    assert.deepEqual(linearSteps(emptyGraph()), []);
  });

  test("a branching puzzle is not linear", () => {
    const g = updateNode(linear3(), "puzzle-1", (n) => ({
      ...n,
      puzzle: { ...n.puzzle!, outputs: [...n.puzzle!.outputs!, { port: "alt", trigger: "answer" }] },
    }));
    assert.equal(linearSteps(g), null);
  });

  test("a hub is not linear", () => {
    let g = linear3();
    g = { ...g, nodes: [...g.nodes, newNode(g, "hub", { x: 0, y: 0 })] };
    assert.equal(linearSteps(g), null);
  });

  // An orphan the author dropped on the canvas is invisible in a list,
  // so its presence must force the canvas.
  test("an orphan node forces the canvas", () => {
    let g = linear3();
    g = { ...g, nodes: [...g.nodes, newNode(g, "puzzle", { x: 0, y: 0 })] };
    assert.equal(linearSteps(g), null);
  });

  test("a line that stops short of the end is not linear", () => {
    assert.equal(linearSteps(disconnect(linear3(), "puzzle-2", "solved")), null);
  });

  test("moving a step reorders by rewiring", () => {
    const g = moveStep(linear3(), "puzzle-2", -1);
    assert.deepEqual(linearSteps(g)?.map((s) => s.slug), ["puzzle-1", "puzzle-2", "story-1"]);
    assert.equal(moveStep(g, "puzzle-1", -1), g, "moving the first step up is a no-op");
  });

  test("removing a step joins its neighbours", () => {
    const g = removeStep(linear3(), "story-1");
    assert.deepEqual(linearSteps(g)?.map((s) => s.slug), ["puzzle-1", "puzzle-2"]);
  });
});

describe("persistence", () => {
  // graph.ParseEnvelope rejects unknown fields, so a stray React Flow
  // property must never survive into a save.
  test("cleanGraph drops fields the server would reject", () => {
    const g = linear3();
    const dirty = {
      ...g,
      nodes: g.nodes.map((n) => ({ ...n, selected: true, measured: { width: 1 } })),
      edges: g.edges.map((e) => ({ ...e, id: "x", animated: true })),
      viewport: { zoom: 1 },
    } as unknown as Graph;
    const clean = JSON.stringify(cleanGraph(dirty));
    for (const bad of ["selected", "measured", "animated", "viewport", '"id"']) {
      assert.equal(clean.includes(bad), false, `${bad} should have been stripped`);
    }
  });

  test("differs notices content and layout changes, not identity", () => {
    const g = linear3();
    assert.equal(differs(g, structuredClone(g)), false);
    assert.equal(differs(g, updateNode(g, "puzzle-1", (n) => ({ ...n, title: "x" }))), true);
    assert.equal(differs(g, updateNode(g, "puzzle-1", (n) => ({ ...n, x: 999 }))), true);
  });
});

describe("tidy", () => {
  test("lays the flow out left to right", () => {
    const g = tidy(linear3());
    const x = (slug: string) => g.nodes.find((n) => n.slug === slug)!.x ?? 0;
    assert.ok(x("start") < x("puzzle-1"));
    assert.ok(x("puzzle-1") < x("story-1"));
    assert.ok(x("puzzle-2") < x("end"));
  });

  // Branches return to their hub. Ranking those back-edges would push the
  // hub to the right of its own branches.
  test("keeps a hub to the left of its looping branches", () => {
    let g = emptyGraph();
    const hub = newNode(g, "hub", { x: 0, y: 0 });
    g = { ...g, nodes: [...g.nodes, hub] };
    const a = newNode(g, "puzzle", { x: 0, y: 0 });
    g = { ...g, nodes: [...g.nodes, a] };
    g = connect(g, "start", "out", hub.slug);
    g = connect(g, hub.slug, "a", a.slug);
    g = connect(g, a.slug, "solved", hub.slug);
    g = connect(g, hub.slug, "done", "end");
    const t = tidy(g);
    const x = (slug: string) => t.nodes.find((n) => n.slug === slug)!.x ?? 0;
    assert.ok(x(hub.slug) < x(a.slug), "the hub should sit left of the branch that loops back to it");
  });
});
