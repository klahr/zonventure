import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { adventureTexts, allTexts, coverage, blockKey, isOutdated, isPortKey, metaKey, nodeKey, nodeTexts, sectionKey, setTranslation, journalKey, sourceFingerprint, trackTranslations, translatedNode, translateSideEdit, translationOriginal, withTexts, zoneKey, itemKey, itemBlockKey, mediaKey, nodeFiles, adventureFiles, translatedFile, isFileOutdated, setTranslatedFile } from "./i18n.ts";
import { translateEdit, updateNode } from "./model.ts";
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

test("a split's group name edited in a translation is a translation, not the main name", () => {
  const split = { slug: "s", type: "split", split: { text: "Dela er", paths: [{ port: "a", label: "Norr" }, { port: "b", label: "Söder" }] } } as Graph["nodes"][number];
  const graph: Graph = { ...g, nodes: [...g.nodes, split] };
  const shown = translatedNode(split, {});
  const edited = { ...shown, split: { ...shown.split!, text: "Split up", paths: shown.split!.paths!.map((p, i) => (i === 0 ? { ...p, label: "North" } : p)) } };
  const out = translateEdit(graph, "en", split, shown, edited)!;
  const step = out.nodes.find((n) => n.slug === "s")!;
  assert.equal(step.split!.text, "Dela er", "the main text stays");
  assert.equal(step.split!.paths![0]!.label, "Norr", "the main group name stays");
  assert.equal(out.translations!.en![nodeKey("s", "split-text")], "Split up");
  assert.equal(out.translations!.en![nodeKey("s", "path:a:label")], "North");
});

test("a port's keys are its own, not those of a port its name begins", () => {
  assert.ok(isPortKey("s", "x", nodeKey("s", "path:x:label")));
  assert.ok(isPortKey("s", "x", nodeKey("s", "out:x:answers")));
  assert.ok(!isPortKey("s", "x", nodeKey("s", "path:x:y:label")), "port x:y is another port");
  assert.ok(isPortKey("s", "x:y", nodeKey("s", "path:x:y:label")));
  assert.ok(!isPortKey("s", "x", nodeKey("s", "path:xy:label")));
  assert.ok(!isPortKey("t", "x", nodeKey("s", "path:x:label")));
  // A branch's picture, keyed by its port alone.
  assert.ok(isPortKey("s", "x", nodeKey("s", "branch:x")));
  assert.ok(!isPortKey("s", "x", nodeKey("s", "branch:xy")));
});

test("a branch removed keeps the picture of a branch still there whose port its name begins", () => {
  const hub = { slug: "h", type: "hub", hub: { branches: [{ port: "x" }, { port: "x:y" }] } } as Graph["nodes"][number];
  const graph: Graph = { ...g, nodes: [...g.nodes, hub], translations: { en: { [nodeKey("h", "branch:x:y")]: "picture", [nodeKey("h", "branch:x:label")]: "X" } } };
  const out = updateNode(graph, "h", (n) => ({ ...n, hub: { ...n.hub!, branches: [{ port: "x:y" }] } }));
  assert.equal(out.translations!.en![nodeKey("h", "branch:x:y")], "picture", "x:y's picture stays");
  assert.equal(out.translations!.en![nodeKey("h", "branch:x:label")], undefined, "x's label goes with it");
});

// The shared vectors the engine's tests read too (internal/graph/session):
// every text an author can translate, each starting with MAIN, and its
// key. The designer must list exactly these, and the engine must show
// every one translated, so the two lists cannot drift apart.
const vectors = JSON.parse(readFileSync(new URL("../testdata/translations.json", import.meta.url), "utf8")) as {
  meta: { title: string; intro: string; outro: string };
  graph: Graph;
  keys: string[];
};

test("allTexts lists every translatable text of the shared vectors, and nothing else", () => {
  const listed = allTexts(vectors.graph, vectors.meta);
  assert.deepEqual(listed.map((x) => x.key).sort(), [...vectors.keys].sort());
  // A journal section's key is its heading itself.
  for (const x of listed) assert.equal(x.main, x.key.startsWith("js:") ? x.key.slice(3) : `MAIN ${x.key}`, `${x.key} should read its own text`);
});

test("withTexts puts a translation into every text nodeTexts lists", () => {
  for (const n of vectors.graph.nodes) {
    const tr = Object.fromEntries(nodeTexts(n).map((x) => [x.key, `XX ${x.key}`]));
    for (const x of nodeTexts(translatedNode(n, tr))) {
      if (!x.answers) assert.equal(x.main, `XX ${x.key}`, `${x.key} of ${n.slug} should be translated`);
    }
  }
});

test("a port removed in a translation takes its wire and its translations with it", () => {
  const sw = { slug: "s", type: "switch", switch: { cases: [{ port: "a" }, { port: "b" }] } } as Graph["nodes"][number];
  const graph: Graph = {
    nodes: [sw, { slug: "x", type: "end" }],
    edges: [{ from: "s", from_port: "a", to: "x" }, { from: "s", from_port: "b", to: "x" }],
    translations: { en: { [nodeKey("s", "out:b:label")]: "B" } },
  };
  const shown = translatedNode(sw, {});
  const out = translateEdit(graph, "en", sw, shown, { ...shown, switch: { cases: [{ port: "a" }] } })!;
  assert.deepEqual(out.edges, [{ from: "s", from_port: "a", to: "x" }], "the removed case's wire goes");
  assert.deepEqual(out.translations!.en, {}, "and so does its translation");
});

test("in a translation, a place renamed after the step gets a translated name, its main name kept", () => {
  const step = { slug: "p", type: "story", title: "Eken", story: { zone: "z" } } as Graph["nodes"][number];
  const before: Graph = { nodes: [step], edges: [], zones: [{ slug: "z", name: "Eken", lat: 0, lng: 0, radius_m: 30 }], translations: { en: {} } };
  const shown = translatedNode(step, {});
  // What retitle writes: the zone follows the shown title.
  const after: Graph = { ...before, zones: [{ ...before.zones![0]!, name: "The oak" }] };
  const out = translateSideEdit(before, after, "en", step, { ...shown, title: "The oak" });
  assert.equal(out.zones![0]!.name, "Eken", "the main name stays");
  assert.equal(out.translations!.en![zoneKey("z")], "The oak");
  assert.equal(translationOriginal(out, "en", zoneKey("z")), "Eken");
  // A grouped place's name is not translated: it is left as it was.
  const grid: Graph = { ...before, zones: [{ ...before.zones![0]!, group: "g" }] };
  const left = translateSideEdit(grid, { ...grid, zones: [{ ...grid.zones![0]!, name: "The oak" }] }, "en", step, shown);
  assert.equal(left.zones![0]!.name, "Eken");
  assert.equal(left.translations!.en![zoneKey("z")], undefined);
});

test("in a translation, a place made where the author stands is named after the step's main title", () => {
  const step = { slug: "p", type: "puzzle", title: "Eken", puzzle: {} } as Graph["nodes"][number];
  const before: Graph = { nodes: [step], edges: [], zones: [] };
  const shown = translatedNode(step, { [nodeKey("p", "title")]: "The oak" });
  const after: Graph = { ...before, zones: [{ slug: "the-oak", name: "The oak", lat: 0, lng: 0, radius_m: 30 }] };
  const out = translateSideEdit(before, after, "en", step, shown);
  assert.equal(out.zones![0]!.name, "Eken");
  assert.equal(out.translations!.en![zoneKey("the-oak")], "The oak");
  // Named something else (a fallback), it keeps that name.
  const other = translateSideEdit(before, { ...before, zones: [{ slug: "place", name: "Place", lat: 0, lng: 0, radius_m: 30 }] }, "en", step, shown);
  assert.equal(other.zones![0]!.name, "Place");
});

test("in a translation, an unnamed place that follows a retitle is named after the main title, the new title its translation", () => {
  const step = { slug: "p", type: "story", title: "Bridge", story: { zone: "z" } } as Graph["nodes"][number];
  const before: Graph = { nodes: [step], edges: [], zones: [{ slug: "z", lat: 0, lng: 0, radius_m: 30 }], translations: { sv: { [nodeKey("p", "title")]: "Bro" } } };
  const shown = translatedNode(step, before.translations!.sv!);
  // What retitle writes for "Bron" typed over "Bro": the zone takes the new title.
  const after: Graph = { ...before, zones: [{ ...before.zones![0]!, name: "Bron" }] };
  const out = translateSideEdit(before, after, "sv", step, { ...shown, title: "Bron" });
  assert.equal(out.zones![0]!.name, "Bridge", "the main name is the main title, not the typed one");
  assert.equal(out.translations!.sv![zoneKey("z")], "Bron");
  assert.equal(translationOriginal(out, "sv", zoneKey("z")), "Bridge");
  // A step without a main title names nothing in the main language.
  const { title: _, ...bare } = step;
  const nameless = translateSideEdit({ ...before, nodes: [bare] }, { ...after, nodes: [bare] }, "sv", bare, { ...bare, title: "Bron" });
  assert.equal(nameless.zones![0]!.name, undefined);
  assert.equal(nameless.translations!.sv![zoneKey("z")], "Bron");
});

test("in a translation, a place or item name cleared loses its translation and keeps its main name", () => {
  const step = { slug: "p", type: "story", title: "Eken", story: { zone: "z" } } as Graph["nodes"][number];
  const before: Graph = {
    nodes: [step], edges: [],
    zones: [{ slug: "z", name: "Eken", lat: 0, lng: 0, radius_m: 30 }],
    items: [{ slug: "key", name: "Nyckel" }],
    translations: { en: { [zoneKey("z")]: "The oak", [itemKey("key", "name")]: "Key" } },
  };
  const after: Graph = { ...before, zones: [{ ...before.zones![0]!, name: "" }], items: [{ slug: "key", name: "" }] };
  const out = translateSideEdit(before, after, "en", step, step);
  assert.equal(out.zones![0]!.name, "Eken");
  assert.deepEqual(out.items, [{ slug: "key", name: "Nyckel" }]);
  assert.deepEqual(out.translations!.en, {});
});

test("in a translation, an item renamed gets a translated name, and one without a name yet its main name", () => {
  const step = { slug: "p", type: "effect", effect: { kind: "grant_item", item: "key" } } as Graph["nodes"][number];
  const before: Graph = { nodes: [step], edges: [], items: [{ slug: "key", name: "Nyckel" }, { slug: "item-1", name: "" }] };
  const after: Graph = { ...before, items: [{ slug: "key", name: "Key" }, { slug: "item-1", name: "Lykta" }] };
  const out = translateSideEdit(before, after, "en", step, step);
  assert.deepEqual(out.items, [{ slug: "key", name: "Nyckel" }, { slug: "item-1", name: "Lykta" }]);
  assert.deepEqual(out.translations!.en, { [itemKey("key", "name")]: "Key" });
  assert.equal(translateSideEdit(before, before, "en", step, step), before, "nothing beside the step changed, nothing to do");
});

test("a step's and an item's pictures, sounds and films can each have a file per language", () => {
  const g: Graph = {
    nodes: [
      {
        slug: "msg",
        type: "story",
        story: {
          blocks: [
            { id: "b1", kind: "audio", media: 10 },
            { id: "b2", kind: "text", text: "Hello" },
            { id: "b3", kind: "call", media: 11, who: "Chief" },
            { id: "b4", kind: "image", media: 0 },
          ],
        },
      },
    ],
    edges: [],
    items: [{ slug: "tape", name: "Tape", blocks: [{ id: "b1", kind: "video", media: 30 }] }],
  };
  assert.deepEqual(
    nodeFiles(g.nodes[0]!).map((f) => [f.key, f.kind, f.main]),
    [
      [mediaKey(blockKey("msg", "b1")), "audio", 10],
      [mediaKey(blockKey("msg", "b3")), "audio", 11],
    ],
  );
  const [film] = adventureFiles(g);
  assert.deepEqual([film!.key, film!.kind, film!.vars], [mediaKey(itemBlockKey("tape", "b1")), "video", { name: "Tape" }]);

  // Not a text: no shared vector, no share of how far a translation has come.
  assert.ok(!allTexts(g).some((x) => x.key.endsWith(":media")));

  const [sound] = nodeFiles(g.nodes[0]!);
  let sv = setTranslatedFile(g, "sv", sound!, 12);
  assert.equal(translatedFile(sv, "sv", sound!), 12);
  assert.equal(sv.translations?.sv?.[sound!.key], "12");
  assert.equal(isFileOutdated(sv, "sv", sound!), false);
  // The original recording replaced: the Swedish one may no longer match it.
  const replaced = nodeFiles({ ...g.nodes[0]!, story: { blocks: [{ id: "b1", kind: "audio", media: 13 }] } })[0]!;
  assert.equal(isFileOutdated(sv, "sv", replaced), true);
  sv = setTranslatedFile(sv, "sv", sound!, undefined);
  assert.equal(translatedFile(sv, "sv", sound!), undefined);
  assert.equal(sv.translations?.sv?.[sound!.key], undefined);
});
