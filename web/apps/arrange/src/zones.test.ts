import { test } from "node:test";
import assert from "node:assert/strict";
import {
  translateEdit,
  translatedNode,
  translateSideEdit,
  translationOriginal,
  zoneKey,
  type Graph,
  type Node,
} from "@zonventure/graph";
import { renameZone, retitle, shownZoneName } from "./designer/zones.ts";

const graph = (zoneName: string | undefined): Graph => ({
  nodes: [
    {
      slug: "oak",
      type: "puzzle",
      title: "Multiple choice",
      puzzle: { zone: "z" },
    } as Node,
  ],
  edges: [],
  zones: [
    {
      slug: "z",
      lat: 59,
      lng: 18,
      radius_m: 30,
      ...(zoneName === undefined ? {} : { name: zoneName }),
    },
  ],
});

test("a zone named after its step follows the step's title", () => {
  const g = graph("Multiple choice");
  const out = retitle(g, g.nodes[0]!, "The big oak");
  assert.equal(out.nodes[0]!.title, "The big oak");
  assert.equal(out.zones![0]!.name, "The big oak");
});

test("a zone with no name takes the title too", () => {
  const g = graph(undefined);
  assert.equal(
    retitle(g, g.nodes[0]!, "The big oak").zones![0]!.name,
    "The big oak",
  );
});

test("a zone the author renamed keeps its name", () => {
  const g = graph("Town square");
  const out = retitle(g, g.nodes[0]!, "The big oak");
  assert.equal(out.nodes[0]!.title, "The big oak");
  assert.equal(out.zones![0]!.name, "Town square");
});

test("a zone with a fallback name follows the title", () => {
  const g = graph("Place");
  assert.equal(
    retitle(g, g.nodes[0]!, "Oak", ["Place"]).zones![0]!.name,
    "Oak",
  );
  assert.equal(retitle(g, g.nodes[0]!, "Oak").zones![0]!.name, "Place");
});

test("in a translation the zone's name is compared as shown, and the edit keeps its main name", () => {
  const g: Graph = { ...graph("Eken"), translations: { en: {} } };
  const step = { ...g.nodes[0]!, title: "Eken" } as Node;
  const main: Graph = { ...g, nodes: [step] };
  // Untranslated, the zone shows its main name, which is the step's as shown.
  const shown = translatedNode(step, {});
  const edited = retitle(main, shown, "The oak", [], { lang: "en", tr: {} });
  assert.equal(
    edited.zones![0]!.name,
    "Eken",
    "retitle follows the shown name, as its translation",
  );
  assert.equal(edited.translations!.en![zoneKey("z")], "The oak");
  // As the Designer applies it.
  const retitled = { ...shown, title: "The oak" };
  const out = translateEdit(
    translateSideEdit(main, edited, "en", step, retitled),
    "en",
    step,
    shown,
    retitled,
  )!;
  assert.equal(out.zones![0]!.name, "Eken", "the main name stays");
  assert.equal(out.nodes[0]!.title, "Eken");
  assert.equal(out.translations!.en![zoneKey("z")], "The oak");
  // A zone with a translated name follows the shown title by that name.
  const tr = { [zoneKey("z")]: "The oak", "n:oak:title": "The oak" };
  // As the Designer applies a retitle in English.
  const apply = (g: Graph, title: string) => {
    const t = g.translations!.en!;
    const shownNow = translatedNode(step, t);
    const edited = { ...shownNow, title };
    return translateEdit(
      translateSideEdit(
        g,
        retitle(g, shownNow, title, [], { lang: "en", tr: t }),
        "en",
        step,
        edited,
      ),
      "en",
      step,
      shownNow,
      edited,
    )!;
  };
  const again = apply({ ...main, translations: { en: tr } }, "Old oak");
  assert.equal(again.translations!.en![zoneKey("z")], "Old oak");
  // Typed back to the main name, the zone's translation still follows.
  const back = apply(again, "Eken");
  assert.equal(back.zones![0]!.name, "Eken");
  assert.equal(back.translations!.en![zoneKey("z")], "Eken");
  assert.equal(
    retitle(main, translatedNode(step, tr), "Old oak").zones![0]!.name,
    "Eken",
    "compared by its main name it doesn't",
  );
});

test("a zone named in a translation gets that language's name, cleared or typed as the main one alike", () => {
  const g = graph("Bro");
  const en = { lang: "en" };
  const named = renameZone(g, g.zones![0]!, "Bridge", en);
  assert.equal(named.zones![0]!.name, "Bro", "the main name stays");
  assert.equal(named.translations!.en![zoneKey("z")], "Bridge");
  assert.equal(translationOriginal(named, "en", zoneKey("z")), "Bro");
  assert.equal(
    shownZoneName(named.zones![0]!, { tr: named.translations!.en! }),
    "Bridge",
  );
  // The main name typed: a name the same in both languages.
  const same = renameZone(named, named.zones![0]!, "Bro", en);
  assert.equal(same.translations!.en![zoneKey("z")], "Bro");
  // Cleared: untranslated again, so it shows its main name.
  const cleared = renameZone(named, named.zones![0]!, "", en);
  assert.equal(cleared.translations!.en![zoneKey("z")], undefined);
  assert.equal(
    shownZoneName(cleared.zones![0]!, { tr: cleared.translations!.en! }),
    "Bro",
  );
  // A zone without a main name is only translated.
  const bare = graph(undefined);
  const out = renameZone(bare, bare.zones![0]!, "Bridge", en);
  assert.equal(out.zones![0]!.name, undefined);
  assert.equal(out.translations!.en![zoneKey("z")], "Bridge");
  // Outside a translation, and for a grouped zone, the zone itself is renamed.
  assert.equal(renameZone(g, g.zones![0]!, "Bridge").zones![0]!.name, "Bridge");
  const grid: Graph = { ...g, zones: [{ ...g.zones![0]!, group: "grid" }] };
  assert.equal(
    renameZone(grid, grid.zones![0]!, "Bridge", en).zones![0]!.name,
    "Bridge",
  );
});
