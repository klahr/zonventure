import { test } from "node:test";
import assert from "node:assert/strict";
import { allTexts, coverage, nodeKey, nodeTexts, setTranslation, journalKey } from "./i18n.ts";
import type { Graph } from "./types.ts";

const g: Graph = {
  language: "sv",
  nodes: [
    { slug: "start", type: "start" },
    { slug: "q", type: "puzzle", title: "Frågan", puzzle: {
      prompt: "Hur många?",
      outputs: [{ port: "solved", label: "Rätt", trigger: "answer", matchers: [{ kind: "exact", value: "sex" }, { kind: "exact", value: "6" }] }],
      responses: [{ text: "Fel." }],
    } },
    { slug: "end", type: "end", end: { text: "Tack!" } },
  ],
  edges: [],
  journal: [{ slug: "carl", title: "Carl", body: "Jobbar här.", section: "Misstänkta" }],
};

test("a step's texts use the engine's keys", () => {
  const keys = nodeTexts(g.nodes[1]!).map((t) => t.key);
  assert.deepEqual(keys, [nodeKey("q", "title"), nodeKey("q", "text"), nodeKey("q", "out:solved:label"), nodeKey("q", "out:solved:answers"), nodeKey("q", "reply:0")]);
  assert.equal(nodeTexts(g.nodes[1]!).find((t) => t.answers)!.main, "sex\n6");
});

test("coverage counts translated texts, not extra answers", () => {
  const total = allTexts(g).filter((t) => !t.answers).length;
  assert.equal(coverage(g, "en"), 0);
  let t = setTranslation(g, "en", nodeKey("q", "text"), "How many?");
  t = setTranslation(t, "en", journalKey("carl", "body"), "Works here.");
  assert.equal(coverage(t, "en"), 2 / total);
  t = setTranslation(t, "en", nodeKey("q", "text"), "  ");
  assert.equal(t.translations!.en![nodeKey("q", "text")], undefined, "an emptied translation is removed");
});
