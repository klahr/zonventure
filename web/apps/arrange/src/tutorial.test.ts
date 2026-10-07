import { test } from "node:test";
import assert from "node:assert/strict";
import type { Graph } from "@zonventure/graph";
import { tutorialProgress, welcomeText } from "./designer/tutorial.ts";

const start: Graph = {
  nodes: [
    { slug: "start", type: "start" },
    { slug: "welcome", type: "story", title: "Erik", story: { blocks: [{ id: "title", kind: "title" }, { id: "b1", kind: "text", text: "Help Erik!" }] } },
    { slug: "end", type: "end", end: { outcome: "complete" } },
  ],
  edges: [
    { from: "start", from_port: "out", to: "welcome" },
    { from: "welcome", from_port: "out", to: "end" },
  ],
} as Graph;

const done = (g: Graph, extra: Partial<{ errors: number; published: boolean }> = {}) =>
  Object.entries(tutorialProgress({ baseline: welcomeText(start), done: [] }, { graph: g, errors: 0, published: false, ...extra }))
    .filter(([, v]) => v)
    .map(([k]) => k);

test("a new tutorial adventure has nothing done yet", () => {
  assert.deepEqual(done(start), []);
});

test("the tutorial's steps are read off the draft", () => {
  const g = structuredClone(start);
  (g.nodes[1]!.story!.blocks![1] as { text: string }).text = "Hi! I'm Erik, the zookeeper.";
  g.zones = [{ slug: "oak", name: "The big oak", lat: 59.3, lng: 18.1, radius_m: 40 }];
  g.nodes.push(
    { slug: "check", type: "story", story: { zone: "oak", blocks: [{ id: "b1", kind: "text", text: "Go to the oak." }] } },
    {
      slug: "count",
      type: "puzzle",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "How many trunks?" }],
        outputs: [{ port: "solved", trigger: "answer", matchers: [{ kind: "exact", value: "7" }] }],
        hints: [{ text: "By the playground", delay_seconds: 120 }],
      },
    },
    {
      slug: "way",
      type: "puzzle",
      puzzle: {
        blocks: [{ id: "b1", kind: "text", text: "Left or right?" }],
        outputs: [
          { port: "a", trigger: "answer", matchers: [{ kind: "exact", value: "left" }] },
          { port: "b", trigger: "answer", matchers: [{ kind: "exact", value: "right" }] },
        ],
      },
    },
  );
  g.edges.push({ from: "way", from_port: "a", to: "end" });
  assert.deepEqual(done(g), ["welcome", "zone", "checkin", "puzzle", "hint"]);
  // A choice counts once both of its answers lead somewhere.
  g.edges.push({ from: "way", from_port: "b", to: "check" });
  assert.deepEqual(done(g), ["welcome", "zone", "checkin", "puzzle", "hint", "choice", "finish"]);
  assert.ok(!done(g, { errors: 1 }).includes("finish"));
  assert.ok(done(g, { published: true }).includes("publish"));
});

test("a step ticked by hand is done", () => {
  const p = tutorialProgress({ baseline: "", done: ["look", "test"] }, { graph: start, errors: 3, published: false });
  assert.equal(p.look, true);
  assert.equal(p.test, true);
  assert.equal(p.zone, false);
});

test("on a phone, a step and the choice are put in place ready-wired", async () => {
  const { addChoiceAfter, addStepAfter, choiceStep } = await import("./designer/tutorial.ts");
  const story = addStepAfter(start, "welcome", "story")!;
  assert.deepEqual(
    story.graph.edges.map((e) => `${e.from}>${e.to}`).sort(),
    ["start>welcome", `welcome>${story.slug}`, `${story.slug}>end`].sort(),
  );
  const c = addChoiceAfter(story.graph, story.slug, { title: "Which way?", left: "left", right: "right", back: "Wrong track", backText: "Back you go!" })!;
  assert.equal(choiceStep(c.graph)?.slug, c.slug);
  const into = (to: string) => c.graph.edges.filter((e) => e.to === to).map((e) => e.from);
  assert.deepEqual(into("end"), [c.slug]);
  const back = c.graph.nodes.find((n) => n.title === "Wrong track")!;
  assert.deepEqual(into(back.slug), [c.slug]);
  assert.ok(into(c.slug).includes(back.slug));
});

test("a step counts once the team has something to read on it", () => {
  const g = structuredClone(start);
  g.zones = [{ slug: "oak", lat: 59.3, lng: 18.1, radius_m: 40 }];
  g.nodes.push(
    { slug: "check", type: "story", story: { zone: "oak", blocks: [{ id: "b1", kind: "text", text: "" }] } },
    { slug: "count", type: "puzzle", puzzle: { outputs: [{ port: "solved", trigger: "answer", matchers: [{ kind: "exact", value: "7" }] }] } },
  );
  assert.deepEqual(done(g), ["zone"]);
  (g.nodes[3]!.story!.blocks![0] as { text: string }).text = "Go to the oak.";
  g.nodes[4]!.puzzle!.blocks = [{ id: "b1", kind: "text", text: "How many?" }];
  assert.deepEqual(done(g), ["zone", "checkin", "puzzle"]);
});
