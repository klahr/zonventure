import { test } from "node:test";
import assert from "node:assert/strict";
import { adventureTexts, allTexts, coverage, blockKey, isOutdated, metaKey, nodeKey, nodeTexts, sectionKey, setTranslation, journalKey, sourceFingerprint, trackTranslations, translatedNode, translateEdit, translationOriginal, withTexts } from "./i18n.ts";
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

test("an order puzzle's cards are texts by index, and translate in place", () => {
  const order = { slug: "o", type: "puzzle" as const, puzzle: {
    answer: { kind: "order" as const, cards: [{ text: "Vasa", color: "#2f6f8f" }, { image: { id: 1, kind: "image" as const } }, { text: "Karl XII" }] },
    outputs: [{ port: "solved", trigger: "answer" as const }],
  } };
  assert.deepEqual(nodeTexts(order).map((t) => t.key), [nodeKey("o", "card:0"), nodeKey("o", "card:2")]);
  const sv = withTexts(order, (k) => (k === nodeKey("o", "card:2") ? "Charles XII" : undefined));
  assert.deepEqual(sv.puzzle!.answer, { ...order.puzzle.answer, cards: [order.puzzle.answer.cards[0], order.puzzle.answer.cards[1], { text: "Charles XII" }] });
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
  assert.equal(t.translation_originals, undefined, "an emptied translation forgets its original");
  const old = setTranslation(g, "en", blockKey("q", "b1"), "How many?");
  assert.ok(!isOutdated(old, "en", allTexts(old).find((x) => x.key === blockKey("q", "b1"))!), "one without a fingerprint is taken as up to date");
});

test("tracking fingerprints old translations from the main text as it is now", () => {
  const text = (x: typeof g) => allTexts(x).find((y) => y.key === blockKey("q", "b1"))!;
  const old = setTranslation(g, "en", blockKey("q", "b1"), "How many?");
  const tracked = trackTranslations(old);
  assert.ok(!isOutdated(tracked, "en", text(tracked)));
  assert.equal(trackTranslations(tracked), tracked, "nothing left to track: the same graph");
  const edited = { ...tracked, nodes: tracked.nodes.map((n) => (n.slug === "q" ? { ...n, puzzle: { ...n.puzzle!, blocks: [{ id: "b1", kind: "text" as const, text: "Hur många!" }] } } : n)) };
  assert.ok(isOutdated(edited, "en", text(edited)), "an edit after tracking marks it outdated");
  assert.equal(trackTranslations(edited), edited, "a kept original is not replaced");
});

test("a translation keeps the original it was made from", () => {
  const t = setTranslation(g, "en", blockKey("q", "b1"), "How many?", "Hur många?");
  assert.equal(translationOriginal(t, "en", blockKey("q", "b1")), "Hur många?");
});

test("tracking turns a fingerprint that still matches into the original, and keeps one that does not", () => {
  const key = blockKey("q", "b1");
  const main = allTexts(g).find((x) => x.key === key)!.main;
  const base = setTranslation(g, "en", key, "How many?");
  const matching = trackTranslations({ ...base, translation_sources: { en: { [key]: sourceFingerprint(main) } } });
  assert.equal(translationOriginal(matching, "en", key), main);
  assert.equal(matching.translation_sources, undefined);
  const stale = { ...base, translation_sources: { en: { [key]: sourceFingerprint("Något annat") } } };
  const kept = trackTranslations(stale);
  assert.equal(translationOriginal(kept, "en", key), undefined);
  assert.ok(isOutdated(kept, "en", allTexts(kept).find((x) => x.key === key)!), "it stays outdated");
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

test("a step shown in a translation reads its translated texts, the original where there is none", () => {
  const q = g.nodes[1]!;
  const shown = translatedNode(q, { [nodeKey("q", "title")]: "The question", [nodeKey("q", "out:solved:label")]: "Right", [blockKey("q", "b1")]: "  " });
  assert.equal(shown.title, "The question");
  assert.equal(shown.puzzle!.outputs![0]!.label, "Right");
  assert.deepEqual(shown.puzzle!.blocks, q.puzzle!.blocks, "an empty translation shows the original");
  assert.equal(q.title, "Frågan", "the step itself is left as it was");
});

test("a step shown in a translation reads every text nodeTexts lists in it", () => {
  const c = { slug: "c", type: "puzzle" as const, title: "Blommor", puzzle: {
    blocks: [{ id: "b1", kind: "chat" as const, lines: [{ who: "Erik", text: "Hej!" }] }],
    responses: [{ text: "Fel." }],
    hints: [{ text: "Titta upp.", delay_seconds: 60 }],
    outputs: [],
  } };
  const tr = Object.fromEntries(nodeTexts(c).map((t) => [t.key, `EN ${t.main}`]));
  const shown = translatedNode(c, tr);
  assert.deepEqual(nodeTexts(shown).map((t) => t.main), nodeTexts(c).map((t) => `EN ${t.main}`));
  assert.equal(shown.puzzle!.hints![0]!.delay_seconds, 60, "what is not text is kept");
  assert.deepEqual(withTexts(c, () => undefined), c, "no text given leaves the step as it was");
});

test("an edit to a step shown in a translation translates its texts and keeps the main ones", () => {
  const q = g.nodes[1]!;
  const tr = { [nodeKey("q", "title")]: "The question" };
  const graph = setTranslation(g, "en", nodeKey("q", "title"), "The question", "Frågan");
  const shown = translatedNode(q, tr);
  const edited = { ...shown, title: "The puzzle", puzzle: { ...shown.puzzle!, responses: [{ text: "Wrong." }], outputs: shown.puzzle!.outputs!.map((o) => ({ ...o, points: 5 })) } };
  const out = translateEdit(graph, "en", q, shown, edited)!;
  const step = out.nodes[1]!;
  assert.equal(step.title, "Frågan", "the main title stays");
  assert.equal(step.puzzle!.responses![0]!.text, "Fel.", "the main reply stays");
  assert.equal(step.puzzle!.outputs![0]!.points, 5, "what is not text changes the step");
  assert.equal(out.translations!.en![nodeKey("q", "title")], "The puzzle");
  assert.equal(out.translations!.en![nodeKey("q", "reply:0")], "Wrong.", "an untranslated text edited becomes its translation");
  assert.equal(translationOriginal(out, "en", nodeKey("q", "reply:0")), "Fel.");
  const fewer = { ...shown, puzzle: { ...shown.puzzle!, responses: [] } };
  assert.equal(translateEdit(graph, "en", q, shown, fewer), null, "removing a text is the main language's to do");
});
