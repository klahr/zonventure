// Translations of an adventure's text. The graph is written in its main
// language — the adventure's own, which the server writes into
// graph.language — and other languages live in graph.translations, keyed
// by field: the same keys the engine reads (graph.NodeKey, ZoneKey,
// JournalKey, RoleKey, SectionKey, MetaKey in Go). A missing translation falls
// back to the main text.

import type { Block, Graph, Node } from "./types.ts";

export const nodeKey = (slug: string, field: string) => `n:${slug}:${field}`;
/** A text block's: graph.BlockKey. */
export const blockKey = (slug: string, id: string) => nodeKey(slug, `block:${id}`);
export const zoneKey = (slug: string) => `z:${slug}`;
export const journalKey = (slug: string, field: string) => `j:${slug}:${field}`;
export const roleKey = (slug: string, field: "name" | "text") => `r:${slug}:${field}`;
/** A journal section's heading: one per section, however many notes share it. */
export const sectionKey = (section: string) => `js:${section}`;
/** The adventure's own title, intro and outro, which live outside the graph. */
export const metaKey = (field: "title" | "intro" | "outro") => `meta:${field}`;
export const TIME_UP_KEY = "time-up";

/** The adventure's own texts, from its settings rather than its graph. */
export interface AdventureMeta {
  title?: string;
  intro?: string;
  outro?: string;
}

/** One piece of text an author can translate. */
export interface Translatable {
  key: string;
  /** What the field is, for the form: "Text", "Hint {{i}}" — an English key, filled from vars. */
  label: string;
  vars?: Record<string, string | number>;
  /** The main-language text, shown beside the translation. */
  main: string;
  multiline?: boolean;
  /** Extra accepted answers, one per line, rather than a translation of text. */
  answers?: boolean;
  /** Worth translating, but not counted in how far a translation has come: a route's name. */
  optional?: boolean;
}

/** Every translatable text of a step, in the order the inspector shows them. */
export function nodeTexts(n: Node): Translatable[] {
  const out: Translatable[] = [];
  const k = (field: string) => nodeKey(n.slug, field);
  const add = (field: string, label: string, main: string | undefined, extra: Partial<Translatable> = {}) => {
    if (main?.trim() || extra.answers) out.push({ key: k(field), label, main: main ?? "", ...extra });
  };
  add("title", "Title", n.title);
  const blocks = (list: Block[] | undefined, label: string) => {
    for (const b of list ?? []) {
      const own = `block:${b.id}`;
      switch (b.kind) {
        case "text":
          add(own, label, b.text, { multiline: true });
          break;
        case "note":
          add(own, "Note", b.text, { multiline: true });
          break;
        case "countdown":
          add(own, "Countdown", b.text);
          break;
        case "level":
        case "noise":
        case "bearing":
        case "shake":
          add(own, "Shown when done", b.text, { multiline: true });
          break;
        case "split":
          b.parts.forEach((p, i) => add(`${own}:part:${i}`, "Secret part {{n}}", p, { multiline: true, vars: { n: i + 1 } }));
          break;
        case "call":
          add(`${own}:who`, "Caller", b.who);
          break;
        case "suspects":
          b.cards.forEach((c, i) => {
            add(`${own}:card:${i}:name`, "Suspect", c.name);
            add(`${own}:card:${i}:text`, "Suspect: facts", c.text, { multiline: true });
          });
          break;
        case "photo":
          add(own, "Photo challenge", b.text, { multiline: true });
          break;
        case "flasher":
          add(own, "Morse message", b.text);
          break;
        case "letters":
          add(own, "Letters", b.text);
          break;
        case "roles":
          add(own, "Roles", b.text, { multiline: true });
          break;
        case "sabotage":
        case "accuse":
          add(own, b.kind === "sabotage" ? "Sabotage" : "Accusation", b.text, { multiline: true });
          break;
        case "vote":
        case "draw":
          add(own, b.kind === "vote" ? "Vote question" : "Draw", b.text, { multiline: true });
          b.options.forEach((o, i) => add(`${own}:option:${i}`, b.kind === "vote" ? "Vote option {{n}}" : "Card {{n}}", o, { vars: { n: i + 1 } }));
          break;
        case "spotdiff":
          (b.spots ?? []).forEach((sp, i) => add(`${own}:spot:${i}`, "Difference", sp.text, { multiline: true }));
          break;
        case "chat":
          b.lines.forEach((l, i) => {
            add(`${own}:who:${i}`, "Chat: who", l.who);
            add(`${own}:line:${i}`, "Chat message", l.text, { multiline: true });
          });
          break;
        case "quiz":
          add(own, "Quiz question", b.text, { multiline: true });
          b.options.forEach((o, i) => add(`${own}:option:${i}`, "Quiz answer {{n}}", o, { vars: { n: ["1", "X", "2"][i] ?? "" } }));
          break;
        case "tiebreak":
          add(own, "Tie-breaker question", b.text, { multiline: true });
          break;
        case "hotspots":
          (b.spots ?? []).forEach((sp, i) => add(`${own}:spot:${i}`, "Hotspot", sp.text, { multiline: true }));
          break;
        case "map":
          (b.markers ?? []).forEach((m, i) => add(`${own}:marker:${i}`, "Map marker", m.label));
          break;
      }
    }
  };
  const p = n.puzzle;
  if (p) {
    blocks(p.blocks, "Text");
    for (const o of p.outputs ?? []) {
      add(`out:${o.port}:label`, "Route “{{name}}”", o.label, { optional: true, vars: { name: o.label || o.port } });
      // Only a typed answer is words a team could give in another
      // language; a lock's, a choice's or a scanned code's values are
      // what the phone sends.
      if (o.trigger !== "answer" || p.answer) continue;
      // Each kind of matcher keeps its own key, so a translated answer is
      // matched the way the original is.
      for (const [kind, field, label] of [["exact", "answers", "Answers for “{{name}}”"], ["contains", "contains", "Words anywhere in the answer for “{{name}}”"]] as const) {
        const values = (o.matchers ?? []).filter((m) => m.kind === kind).map((m) => m.value ?? "");
        if (values.length) add(`out:${o.port}:${field}`, label, values.join("\n"), { multiline: true, answers: true, vars: { name: o.label || o.port } });
      }
    }
    add("bar:placeholder", "Answer bar: placeholder", p.bar?.placeholder);
    add("bar:button", "Answer bar: button", p.bar?.button);
    (p.responses ?? []).forEach((r, i) => add(`reply:${i}`, "Wrong-answer reply {{n}}", r.text, { multiline: true, vars: { n: i + 1 } }));
    (p.hints ?? []).forEach((h, i) => add(`hint:${i}`, "Hint {{i}}", h.text, { multiline: true, vars: { i: i + 1 } }));
    if (p.answer?.kind === "choice") {
      p.answer.fields.forEach((f, i) => {
        add(`field:${i}:label`, "Question {{n}}", f.label, { vars: { n: i + 1 } });
        for (const o of f.options) add(`field:${i}:opt:${o.value}`, "Question {{n}}: option", o.label || o.value, { vars: { n: i + 1 } });
      });
    }
    if (p.answer?.kind === "lock") {
      for (const s of p.answer.symbols) add(`sym:${s.value}`, "Lock symbol", s.label || (s.image ? "" : s.value));
    }
    if (p.answer?.kind === "items") {
      p.answer.slots.forEach((s, i) => add(`slot:${i}`, "Slot {{n}}", s.label, { vars: { n: i + 1 } }));
    }
  }
  if (n.story) blocks(n.story.blocks, "Text");
  if (n.end) blocks(n.end.blocks, "Closing text");
  if (n.hub) {
    add("hub-text", "Text", n.hub.text, { multiline: true });
    for (const b of n.hub.branches ?? []) {
      add(`branch:${b.port}:label`, "Branch “{{name}}”", b.label, { vars: { name: b.label || b.port } });
      add(`branch:${b.port}:note`, "Branch “{{name}}”: subtitle", b.note, { vars: { name: b.label || b.port } });
    }
  }
  return out;
}

/**
 * The step as it reads in a translation, for showing it: its title, text
 * and route names in that language where they are translated, the
 * original elsewhere. Only for display — the graph keeps the main texts.
 */
export function translatedNode(n: Node, tr: Record<string, string>): Node {
  const k = (field: string) => tr[nodeKey(n.slug, field)]?.trim() || undefined;
  const blocks = (list: Block[] | undefined) =>
    list?.map((b) => ((b.kind === "text" || b.kind === "note") && k(`block:${b.id}`) ? { ...b, text: k(`block:${b.id}`)! } : b));
  const out: Node = { ...n, ...(k("title") ? { title: k("title")! } : {}) };
  if (n.puzzle)
    out.puzzle = {
      ...n.puzzle,
      ...(n.puzzle.blocks ? { blocks: blocks(n.puzzle.blocks)! } : {}),
      ...(n.puzzle.outputs ? { outputs: n.puzzle.outputs.map((o) => (k(`out:${o.port}:label`) ? { ...o, label: k(`out:${o.port}:label`)! } : o)) } : {}),
    };
  if (n.story?.blocks) out.story = { ...n.story, blocks: blocks(n.story.blocks)! };
  if (n.end?.blocks) out.end = { ...n.end, blocks: blocks(n.end.blocks)! };
  if (n.hub)
    out.hub = {
      ...n.hub,
      ...(k("hub-text") ? { text: k("hub-text")! } : {}),
      ...(n.hub.branches
        ? {
            branches: n.hub.branches.map((b) => ({
              ...b,
              ...(k(`branch:${b.port}:label`) ? { label: k(`branch:${b.port}:label`)! } : {}),
              ...(k(`branch:${b.port}:note`) ? { note: k(`branch:${b.port}:note`)! } : {}),
            })),
          }
        : {}),
    };
  return out;
}

/**
 * The adventure-wide texts: its title, intro and outro, journal notes,
 * zone names, the time-up message.
 */
export function adventureTexts(g: Graph, meta: AdventureMeta = {}): Translatable[] {
  const out: Translatable[] = [];
  if (meta.title?.trim()) out.push({ key: metaKey("title"), label: "Adventure title", main: meta.title });
  if (meta.intro?.trim()) out.push({ key: metaKey("intro"), label: "Introduction", main: meta.intro, multiline: true });
  if (meta.outro?.trim()) out.push({ key: metaKey("outro"), label: "Closing words", main: meta.outro, multiline: true });
  for (const j of g.journal ?? []) {
    if (j.title?.trim()) out.push({ key: journalKey(j.slug, "title"), label: "Journal: {{name}}", vars: { name: j.title }, main: j.title });
    if (j.body?.trim()) out.push({ key: journalKey(j.slug, "body"), label: "Journal: {{name}}", vars: { name: j.title || j.slug }, main: j.body, multiline: true });
  }
  for (const s of new Set((g.journal ?? []).map((j) => j.section).filter((x): x is string => !!x))) {
    out.push({ key: sectionKey(s), label: "Journal section", main: s });
  }
  for (const z of g.zones ?? []) if (z.name?.trim() && !z.group) out.push({ key: zoneKey(z.slug), label: "Place name", main: z.name });
  for (const r of g.roles ?? []) {
    if (r.name?.trim()) out.push({ key: roleKey(r.slug, "name"), label: "Role", main: r.name });
    if (r.text?.trim()) out.push({ key: roleKey(r.slug, "text"), label: "Role: {{name}}", vars: { name: r.name || r.slug }, main: r.text, multiline: true });
    (r.goals ?? []).forEach((g, i) => {
      if (g.text.trim()) out.push({ key: `r:${r.slug}:goal:${i}`, label: "Goal of {{name}}", vars: { name: r.name || r.slug }, main: g.text });
    });
  }
  if (g.time_limit?.text?.trim()) out.push({ key: TIME_UP_KEY, label: "Time is up: message", main: g.time_limit.text, multiline: true });
  return out;
}

/** Every translatable text of the adventure. */
export function allTexts(g: Graph, meta: AdventureMeta = {}): Translatable[] {
  return [...g.nodes.flatMap(nodeTexts), ...adventureTexts(g, meta)];
}

/** A short fingerprint of a main text (FNV-1a), kept beside a translation made from it. */
export function sourceFingerprint(main: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < main.length; i++) h = Math.imul(h ^ main.charCodeAt(i), 0x01000193);
  return (h >>> 0).toString(16).padStart(8, "0");
}

/**
 * Whether a translation was made from a main text that has changed since.
 * One saved before fingerprints were kept is taken as up to date.
 */
export function isOutdated(g: Graph, lang: string, t: Translatable): boolean {
  const from = g.translation_sources?.[lang]?.[t.key];
  return !t.answers && !!g.translations?.[lang]?.[t.key]?.trim() && !!from && from !== sourceFingerprint(t.main);
}

/** How much of the adventure is translated into lang, 0–1. An outdated translation does not count. */
export function coverage(g: Graph, lang: string, meta: AdventureMeta = {}): number {
  const all = allTexts(g, meta).filter((t) => !t.answers && !t.optional);
  if (!all.length) return 1;
  const tr = g.translations?.[lang] ?? {};
  return all.filter((t) => tr[t.key]?.trim() && !isOutdated(g, lang, t)).length / all.length;
}

/**
 * The graph with one translation set, made from main, or cleared when text
 * is empty. Setting the same text again marks it up to date with main.
 */
export function setTranslation(g: Graph, lang: string, key: string, text: string, main?: string): Graph {
  const all = { ...(g.translations ?? {}) };
  const one = { ...(all[lang] ?? {}) };
  const sources = { ...(g.translation_sources ?? {}) };
  const from = { ...(sources[lang] ?? {}) };
  if (text.trim()) one[key] = text;
  else delete one[key];
  if (text.trim() && main !== undefined) from[key] = sourceFingerprint(main);
  else delete from[key];
  all[lang] = one;
  if (Object.keys(from).length) sources[lang] = from;
  else delete sources[lang];
  const out: Graph = { ...g, translations: all, translation_sources: sources };
  if (!Object.keys(sources).length) delete out.translation_sources;
  return out;
}
