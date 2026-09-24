import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  gridZones,
  zoneContains,
  answerOptions,
  appendStep,
  insertAfter,
  insertionPoint,
  STEP_PITCH,
  referAnswer,
  resolveAnswer,
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

  // Tidy keeps an author's grouping: steps stay in their frame, frames
  // fit their steps, and a frame that grows pushes the next one down.
  test("keeps steps inside their frames", () => {
    const story = (slug: string, x: number, y: number) => ({ slug, type: "story" as const, x, y, story: { text: "" } });
    const g: Graph = {
      nodes: [story("a1", 0, 60), story("a2", 20, 70), story("a3", 40, 80), story("b1", 0, 360), story("b2", 30, 370)],
      edges: [
        { from: "a1", from_port: "out", to: "a2" },
        { from: "a2", from_port: "out", to: "a3" },
        { from: "b1", from_port: "out", to: "b2" },
      ],
      frames: [
        { id: "A", x: -50, y: 0, width: 400, height: 250 },
        { id: "B", x: -50, y: 300, width: 400, height: 250 },
      ],
    };
    const t = tidy(g);
    const node = (s: string) => t.nodes.find((n) => n.slug === s)!;
    const frame = (id: string) => t.frames!.find((f) => f.id === id)!;
    const inFrame = (s: string, id: string) => {
      const n = node(s), f = frame(id);
      return n.x! >= f.x && n.x! + 240 <= f.x + f.width && n.y! >= f.y && n.y! + 110 <= f.y + f.height;
    };
    for (const s of ["a1", "a2", "a3"]) assert.ok(inFrame(s, "A"), `${s} stays in A`);
    for (const s of ["b1", "b2"]) assert.ok(inFrame(s, "B"), `${s} stays in B`);
    assert.ok(node("a1").x! < node("a2").x! && node("a2").x! < node("a3").x!, "laid out left to right inside the frame");
    assert.ok(frame("A").width > 400, "A grew to fit three steps in a row");
    assert.ok(frame("B").y >= frame("A").y + frame("A").height, "B is pushed below A");
  });
});

test("a crossroads reference follows the answer through a spelling fix", () => {
  let g: Graph = {
    nodes: [
      { slug: "q", type: "puzzle", puzzle: { outputs: [{ port: "ok", trigger: "answer", matchers: [{ kind: "exact", value: "nroth" }, { kind: "anything" }] }] } },
    ],
    edges: [],
  };
  const opts = answerOptions(g, "q");
  assert.equal(opts.length, 1, "“any answer” is not something to route on");
  const { graph, ref } = referAnswer(g, opts[0]!);
  assert.equal(ref, "q/a1");
  g = updateNode(graph, "q", (n) => ({
    ...n,
    puzzle: { outputs: [{ port: "ok", trigger: "answer", matchers: [{ id: "a1", kind: "exact", value: "north" }] }] },
  }));
  assert.equal(resolveAnswer(g, ref)?.label, "north");
  assert.equal(referAnswer(g, answerOptions(g, "q")[0]!).graph, g, "an answer with an id is left alone");
});

test("zoneContains matches the server's circle and polygon tests", () => {
  const circle = { slug: "c", lat: 59.3, lng: 18.0, radius_m: 10 };
  assert.equal(zoneContains(circle, 59.30005, 18.0), true); // ~5.6 m
  assert.equal(zoneContains(circle, 59.3002, 18.0), false); // ~22 m
  const square = { slug: "s", lat: 0, lng: 0, radius_m: 0, polygon: [[0, 0], [0, 1], [1, 1], [1, 0]] as [number, number][] };
  assert.equal(zoneContains(square, 0.5, 0.5), true);
  assert.equal(zoneContains(square, 1.5, 0.5), false);
});

test("gridZones cuts an area into lettered cells that tile it", () => {
  const cells = gridZones([[1, 0], [1, 3], [0, 3], [0, 0]], 2, 3, "Scanner");
  assert.equal(cells.length, 6);
  assert.deepEqual(cells.map((z) => z.name), ["A1", "B1", "C1", "A2", "B2", "C2"]);
  assert.equal(cells[0]!.slug, "scanner-a1");
  assert.equal(cells[0]!.group, "Scanner");
  // Every point of the area is in exactly one cell.
  for (const [lat, lng] of [[0.9, 0.1], [0.6, 1.5], [0.1, 2.9], [0.4, 0.2]] as const) {
    assert.equal(cells.filter((z) => zoneContains(z, lat, lng)).length, 1, `${lat},${lng}`);
  }
  assert.equal(zoneContains(cells[0]!, 0.9, 0.1), true, "top-left is A1");
  const again = gridZones([[1, 0], [1, 3], [0, 3], [0, 0]], 1, 1, "Scanner", cells);
  assert.equal(again[0]!.slug, "scanner-a1-2", "slugs never collide");
});

describe("adding a step without a wire", () => {
  const puzzle = (g: Graph) => newNode(g, "puzzle", { x: 0, y: 0 });

  test("a blank adventure gains its first step between start and end", () => {
    const g = emptyGraph();
    const at = insertionPoint(g, null);
    assert.deepEqual(at, { from: "start", port: "out" });
    const p = puzzle(g);
    const out = insertAfter(g, at!, p);
    const edges = out.edges.map((e) => `${e.from}.${e.from_port}>${e.to}`).sort();
    assert.deepEqual(edges, [`${p.slug}.solved>end`, `start.out>${p.slug}`]);
    const pos = (slug: string) => out.nodes.find((n) => n.slug === slug)!;
    assert.equal(pos(p.slug).x, STEP_PITCH, "a column right of the start");
    assert.equal(pos("end").x, 2 * STEP_PITCH, "the end moves over to make room");
    assert.equal(pos(p.slug).y, 0);
  });

  test("with a step selected, the new one follows it", () => {
    let g = emptyGraph();
    const a = puzzle(g);
    g = insertAfter(g, insertionPoint(g, null)!, a);
    assert.deepEqual(insertionPoint(g, a.slug), { from: a.slug, port: "solved" });
    assert.deepEqual(insertionPoint(g, "end"), { from: a.slug, port: "solved" }, "an end: just before it");
  });

  test("frames around the insertion widen, frames further right move", () => {
    const g: Graph = {
      ...emptyGraph(),
      frames: [
        { id: "around", x: -20, y: -20, width: 300, height: 200 },
        { id: "right", x: 2000, y: 0, width: 100, height: 100 },
      ],
    };
    const out = insertAfter(g, { from: "start", port: "out" }, puzzle(g));
    assert.equal(out.frames![0]!.width, 300 + STEP_PITCH);
    assert.equal(out.frames![1]!.x, 2000 + STEP_PITCH);
  });

  test("nowhere to attach is null", () => {
    assert.equal(insertionPoint({ nodes: [{ slug: "start", type: "start" }], edges: [] }, null), null);
  });
});
