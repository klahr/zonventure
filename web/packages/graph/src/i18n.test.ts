import { test } from "node:test";
import assert from "node:assert/strict";
import { adventureTexts, allTexts, coverage, blockKey, isOutdated, metaKey, nodeKey, nodeTexts, sectionKey, setTranslation, journalKey } from "./i18n.ts";
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

test("a field's label is a translatable key with its values apart", () => {
  const answers = nodeTexts(g.nodes[1]!).find((t) => t.key === nodeKey("q", "out:solved:answers"))!;
  assert.equal(answers.label, "Answers for “{{name}}”");
  assert.deepEqual(answers.vars, { name: "Rätt" });
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

test("coverage counts translated texts, not extra answers or route names", () => {
  const total = allTexts(g).filter((t) => !t.answers && !t.optional).length;
  assert.ok(allTexts(g).find((t) => t.key === nodeKey("q", "out:solved:label"))?.optional);
  assert.equal(coverage(g, "en"), 0);
  let t = setTranslation(g, "en", blockKey("q", "b1"), "How many?");
  t = setTranslation(t, "en", journalKey("carl", "body"), "Works here.");
  assert.equal(coverage(t, "en"), 2 / total);
  t = setTranslation(t, "en", blockKey("q", "b1"), "  ");
  assert.equal(t.translations!.en![blockKey("q", "b1")], undefined, "an emptied translation is removed");
});

test("a translation of a changed original is outdated and not counted", () => {
  const text = () => allTexts(t).find((x) => x.key === blockKey("q", "b1"))!;
  let t = setTranslation(g, "en", blockKey("q", "b1"), "How many?", "Hur många?");
  const once = coverage(t, "en");
  assert.ok(once > 0 && !isOutdated(t, "en", text()));
  t = { ...t, nodes: t.nodes.map((n) => (n.slug === "q" ? { ...n, puzzle: { ...n.puzzle!, blocks: [{ id: "b1", kind: "text" as const, text: "Hur många finns?" }] } } : n)) };
  assert.ok(isOutdated(t, "en", text()));
  assert.equal(coverage(t, "en"), 0);
  t = setTranslation(t, "en", blockKey("q", "b1"), "How many?", text().main);
  assert.ok(!isOutdated(t, "en", text()), "set again from the new original, it is up to date");
  assert.equal(coverage(t, "en"), once);
  t = setTranslation(t, "en", blockKey("q", "b1"), "");
  assert.equal(t.translation_sources, undefined, "an emptied translation forgets its original");
  const old = setTranslation(g, "en", blockKey("q", "b1"), "How many?");
  assert.ok(!isOutdated(old, "en", allTexts(old).find((x) => x.key === blockKey("q", "b1"))!), "one without a fingerprint is taken as up to date");
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

test("the answer bar's words are translated where the engine reads them", () => {
  const p = { slug: "p", type: "puzzle" as const, puzzle: {
    bar: { placeholder: "Årtalet", button: "Gissa", keyboard: "number" as const },
    outputs: [{ port: "ok", trigger: "answer" as const, matchers: [{ kind: "exact" as const, value: "1887" }] }],
  } };
  const keys = nodeTexts(p).map((t) => t.key);
  assert.ok(keys.includes(nodeKey("p", "bar:placeholder")) && keys.includes(nodeKey("p", "bar:button")), keys.join(" "));
});
