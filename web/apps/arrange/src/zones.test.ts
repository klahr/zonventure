import { test } from "node:test";
import assert from "node:assert/strict";
import type { Graph, Node } from "@zonventure/graph";
import { retitle } from "./designer/zones.ts";

const graph = (zoneName: string | undefined): Graph => ({
  nodes: [{ slug: "oak", type: "puzzle", title: "Multiple choice", puzzle: { zone: "z" } } as Node],
  edges: [],
  zones: [{ slug: "z", lat: 59, lng: 18, radius_m: 30, ...(zoneName === undefined ? {} : { name: zoneName }) }],
});

test("a zone named after its step follows the step's title", () => {
  const g = graph("Multiple choice");
  const out = retitle(g, g.nodes[0]!, "The big oak");
  assert.equal(out.nodes[0]!.title, "The big oak");
  assert.equal(out.zones![0]!.name, "The big oak");
});

test("a zone with no name takes the title too", () => {
  const g = graph(undefined);
  assert.equal(retitle(g, g.nodes[0]!, "The big oak").zones![0]!.name, "The big oak");
});

test("a zone the author renamed keeps its name", () => {
  const g = graph("Town square");
  const out = retitle(g, g.nodes[0]!, "The big oak");
  assert.equal(out.nodes[0]!.title, "The big oak");
  assert.equal(out.zones![0]!.name, "Town square");
});

test("a zone with a fallback name follows the title", () => {
  const g = graph("Place");
  assert.equal(retitle(g, g.nodes[0]!, "Oak", ["Place"]).zones![0]!.name, "Oak");
  assert.equal(retitle(g, g.nodes[0]!, "Oak").zones![0]!.name, "Place");
});
