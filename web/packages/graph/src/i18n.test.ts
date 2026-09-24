import { test } from "node:test";
import assert from "node:assert/strict";
import { adventureTexts, allTexts, coverage, blockKey, metaKey, nodeKey, nodeTexts, sectionKey, setTranslation, journalKey } from "./i18n.ts";
import type { Graph } from "./types.ts";

const g: Graph = {
  language: "sv",
  nodes: [
    { slug: "start", type: "start" },
    { slug: "q", type: "puzzle", title: "Frågan", puzzle: {
      blocks: [{ id: "b1", kind: "text", text: "Hur många?" }],
      outputs: [{ port: "solved", label: "Rätt", trigger: "answer", matchers: [{ kind: "exact", value: "sex" }, { kind: "exact", value: "6" }, { kind: "contains", value: "sexton" }] }],
      responses: [{ text: "Fel." }],
    } },
    { slug: "end", type: "end", end: { blocks: [{ id: "b1", kind: "text", text: "Tack!" }] } },
  ],
  edges: [],
  journal: [{ slug: "carl", title: "Carl", body: "Jobbar här.", section: "Misstänkta" }],
};

test("a step's texts use the engine's keys", () => {
  const keys = nodeTexts(g.nodes[1]!).map((t) => t.key);
  assert.deepEqual(keys, [
    nodeKey("q", "title"), blockKey("q", "b1"), nodeKey("q", "out:solved:label"),
    nodeKey("q", "out:solved:answers"), nodeKey("q", "out:solved:contains"), nodeKey("q", "reply:0"),
  ]);
  const answers = nodeTexts(g.nodes[1]!).filter((t) => t.answers);
  assert.deepEqual(answers.map((t) => t.main), ["sex\n6", "sexton"], "exact and contains answers are translated apart");
});

test("a lock's values are not answers to translate", () => {
  const lock = { slug: "l", type: "puzzle" as const, puzzle: {
    answer: { kind: "lock" as const, wheels: 1, symbols: [{ value: "owl", label: "Uggla" }, { value: "fox" }] },
    outputs: [{ port: "open", trigger: "answer" as const, matchers: [{ kind: "exact" as const, value: "owl" }] }],
  } };
  const keys = nodeTexts(lock).map((t) => t.key);
  assert.ok(!keys.includes(nodeKey("l", "out:open:answers")));
  assert.deepEqual(keys, [nodeKey("l", "sym:owl"), nodeKey("l", "sym:fox")]);
});

test("adventure texts: meta, sections by name", () => {
  const keys = adventureTexts(g, { title: "Mysteriet", intro: "" }).map((t) => t.key);
  assert.ok(keys.includes(metaKey("title")) && !keys.includes(metaKey("intro")));
  assert.ok(keys.includes(sectionKey("Misstänkta")));
});

test("coverage counts translated texts, not extra answers", () => {
  const total = allTexts(g).filter((t) => !t.answers).length;
  assert.equal(coverage(g, "en"), 0);
  let t = setTranslation(g, "en", blockKey("q", "b1"), "How many?");
  t = setTranslation(t, "en", journalKey("carl", "body"), "Works here.");
  assert.equal(coverage(t, "en"), 2 / total);
  t = setTranslation(t, "en", blockKey("q", "b1"), "  ");
  assert.equal(t.translations!.en![blockKey("q", "b1")], undefined, "an emptied translation is removed");
});

test("block texts use the engine's block keys", () => {
  const n = { slug: "s", type: "story" as const, story: { blocks: [
    { id: "c", kind: "chat" as const, lines: [{ who: "Anna", text: "Hej" }, { text: "Hallå?", me: true }] },
    { id: "m", kind: "map" as const, zone: "z", markers: [{ lat: 1, lng: 2, label: "Kyrkan" }] },
    { id: "d", kind: "divider" as const },
  ] } };
  assert.deepEqual(nodeTexts(n).map((t) => t.key), [
    blockKey("s", "c") + ":who:0", blockKey("s", "c") + ":line:0", blockKey("s", "c") + ":line:1",
    blockKey("s", "m") + ":marker:0",
  ]);
});
