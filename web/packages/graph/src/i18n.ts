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
 * The step with its texts replaced where text gives one: f is asked for
 * every text nodeTexts lists, by the same key, and leaves the rest alone.
 * Accepted answers are not texts here: a translation only adds to them.
 */
export function withTexts(n: Node, f: (key: string) => string | undefined): Node {
  const k = (field: string) => f(nodeKey(n.slug, field));
  const or = (field: string, was: string) => k(field) ?? was;
  const opt = (field: string, was: string | undefined) => (was === undefined ? undefined : or(field, was));
  const block = (b: Block): Block => {
    const own = `block:${b.id}`;
    switch (b.kind) {
      case "text":
      case "note":
      case "countdown":
      case "level":
      case "noise":
      case "bearing":
      case "shake":
      case "photo":
      case "flasher":
      case "letters":
      case "roles":
      case "sabotage":
      case "accuse":
      case "tiebreak":
        return b.text === undefined || k(own) === undefined ? b : { ...b, text: k(own)! };
      case "split":
        return { ...b, parts: b.parts.map((p, i) => or(`${own}:part:${i}`, p)) };
      case "call":
        return b.who === undefined || k(`${own}:who`) === undefined ? b : { ...b, who: k(`${own}:who`)! };
      case "suspects":
        return { ...b, cards: b.cards.map((c, i) => ({ ...c, name: or(`${own}:card:${i}:name`, c.name), ...(c.text !== undefined ? { text: or(`${own}:card:${i}:text`, c.text) } : {}) })) };
      case "vote":
      case "draw":
        return { ...b, ...(b.text !== undefined ? { text: or(own, b.text) } : {}), options: b.options.map((o, i) => or(`${own}:option:${i}`, o)) };
      case "quiz": {
        const [a, x, c] = b.options.map((o, i) => or(`${own}:option:${i}`, o));
        return { ...b, ...(b.text !== undefined ? { text: or(own, b.text) } : {}), options: [a!, x!, c!] };
      }
      case "spotdiff":
      case "hotspots":
        return b.spots ? { ...b, spots: b.spots.map((sp, i) => (sp.text === undefined ? sp : { ...sp, text: or(`${own}:spot:${i}`, sp.text) })) } : b;
      case "chat":
        return { ...b, lines: b.lines.map((l, i) => ({ ...l, ...(l.who !== undefined ? { who: or(`${own}:who:${i}`, l.who) } : {}), text: or(`${own}:line:${i}`, l.text) })) };
      case "map":
        return b.markers ? { ...b, markers: b.markers.map((m, i) => (m.label === undefined ? m : { ...m, label: or(`${own}:marker:${i}`, m.label) })) } : b;
      default:
        return b;
    }
  };
  const blocks = (list: Block[] | undefined) => list?.map(block);
  const title = opt("title", n.title);
  const out: Node = { ...n, ...(title !== undefined ? { title } : {}) };
  const p = n.puzzle;
  if (p) {
    const bar = p.bar && { ...p.bar, ...(p.bar.placeholder !== undefined ? { placeholder: or("bar:placeholder", p.bar.placeholder) } : {}), ...(p.bar.button !== undefined ? { button: or("bar:button", p.bar.button) } : {}) };
    let answer = p.answer;
    if (answer?.kind === "choice")
      answer = { ...answer, fields: answer.fields.map((fl, i) => ({ ...fl, ...(fl.label !== undefined ? { label: or(`field:${i}:label`, fl.label) } : {}), options: fl.options.map((o) => (k(`field:${i}:opt:${o.value}`) === undefined ? o : { ...o, label: k(`field:${i}:opt:${o.value}`)! })) })) };
    else if (answer?.kind === "lock")
      answer = { ...answer, symbols: answer.symbols.map((sy) => (k(`sym:${sy.value}`) === undefined ? sy : { ...sy, label: k(`sym:${sy.value}`)! })) };
    else if (answer?.kind === "items")
      answer = { ...answer, slots: answer.slots.map((sl, i) => (sl.label === undefined ? sl : { ...sl, label: or(`slot:${i}`, sl.label) })) };
    out.puzzle = {
      ...p,
      ...(p.blocks ? { blocks: blocks(p.blocks)! } : {}),
      ...(p.outputs ? { outputs: p.outputs.map((o) => (o.label === undefined ? o : { ...o, label: or(`out:${o.port}:label`, o.label) })) } : {}),
      ...(bar ? { bar } : {}),
      ...(p.responses ? { responses: p.responses.map((r, i) => ({ ...r, text: or(`reply:${i}`, r.text) })) } : {}),
      ...(p.hints ? { hints: p.hints.map((h, i) => ({ ...h, text: or(`hint:${i}`, h.text) })) } : {}),
      ...(answer ? { answer } : {}),
    };
  }
  if (n.story?.blocks) out.story = { ...n.story, blocks: blocks(n.story.blocks)! };
  if (n.end?.blocks) out.end = { ...n.end, blocks: blocks(n.end.blocks)! };
  if (n.hub)
    out.hub = {
      ...n.hub,
      ...(n.hub.text !== undefined ? { text: or("hub-text", n.hub.text) } : {}),
      ...(n.hub.branches
        ? {
            branches: n.hub.branches.map((b) => ({
              ...b,
              ...(b.label !== undefined ? { label: or(`branch:${b.port}:label`, b.label) } : {}),
              ...(b.note !== undefined ? { note: or(`branch:${b.port}:note`, b.note) } : {}),
            })),
          }
        : {}),
    };
  return out;
}

/**
 * The step as it reads in a translation, for showing it: every text in
 * that language where it is translated, the original elsewhere. Only for
 * display — the graph keeps the main texts.
 */
export function translatedNode(n: Node, tr: Record<string, string>): Node {
  return withTexts(n, (key) => tr[key]?.trim() || undefined);
}

/**
 * An edit made to a step shown in a translation (translatedNode): the
 * texts changed become that language's translations, and anything else
 * changes the step itself, whose own texts stay as they were. Null when
 * the edit adds or removes texts — a block, a chat line, a hint — which
 * is the main language's to do: keys counted by position would land on
 * the wrong text.
 */
export function translateEdit(g: Graph, lang: string, main: Node, shown: Node, edited: Node): Graph | null {
  const texts = (n: Node) => new Map(nodeTexts(n).filter((t) => !t.answers).map((t) => [t.key, t.main]));
  const before = texts(shown);
  const after = texts(edited);
  const own = texts(main);
  if (before.size !== after.size || [...before.keys()].some((key) => !after.has(key))) return null;
  const step = withTexts(edited, (key) => (after.get(key) !== own.get(key) ? own.get(key) : undefined));
  let out: Graph = { ...g, nodes: g.nodes.map((n) => (n.slug === main.slug ? step : n)) };
  for (const [key, text] of after) {
    if (text !== before.get(key)) out = setTranslation(out, lang, key, text, own.get(key));
  }
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

/**
 * A short fingerprint of a main text (FNV-1a), kept beside translations
 * made before their originals were. See translationOriginal.
 */
export function sourceFingerprint(main: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < main.length; i++) h = Math.imul(h ^ main.charCodeAt(i), 0x01000193);
  return (h >>> 0).toString(16).padStart(8, "0");
}

/** The main text a translation was made from, when it was kept. */
export function translationOriginal(g: Graph, lang: string, key: string): string | undefined {
  return g.translation_originals?.[lang]?.[key];
}

/**
 * Whether a translation was made from a main text that has changed since,
 * by its original or else its fingerprint. One saved before either was
 * kept is taken as up to date.
 */
export function isOutdated(g: Graph, lang: string, t: Translatable): boolean {
  if (t.answers || !g.translations?.[lang]?.[t.key]?.trim()) return false;
  const was = translationOriginal(g, lang, t.key);
  if (was !== undefined) return was !== t.main;
  const from = g.translation_sources?.[lang]?.[t.key];
  return !!from && from !== sourceFingerprint(t.main);
}

/**
 * The graph with each translation that has no original kept given the main
 * text as it is now, or g itself when none lacks one: a translation kept
 * before originals were is then marked outdated by later edits. One whose
 * fingerprint no longer matches keeps it, so it stays outdated.
 */
export function trackTranslations(g: Graph, meta: AdventureMeta = {}): Graph {
  const originals = { ...(g.translation_originals ?? {}) };
  const sources = { ...(g.translation_sources ?? {}) };
  let added = false;
  const texts = allTexts(g, meta);
  for (const [lang, tr] of Object.entries(g.translations ?? {})) {
    const was = { ...(originals[lang] ?? {}) };
    const from = { ...(sources[lang] ?? {}) };
    for (const t of texts) {
      if (!tr[t.key]?.trim() || was[t.key] !== undefined) continue;
      if (from[t.key] && from[t.key] !== sourceFingerprint(t.main)) continue;
      was[t.key] = t.main;
      delete from[t.key];
      added = true;
    }
    if (Object.keys(was).length) originals[lang] = was;
    if (Object.keys(from).length) sources[lang] = from;
    else delete sources[lang];
  }
  if (!added) return g;
  const out: Graph = { ...g, translation_originals: originals, translation_sources: sources };
  if (!Object.keys(sources).length) delete out.translation_sources;
  return out;
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
  const originals = { ...(g.translation_originals ?? {}) };
  const was = { ...(originals[lang] ?? {}) };
  const sources = { ...(g.translation_sources ?? {}) };
  const from = { ...(sources[lang] ?? {}) };
  if (text.trim()) one[key] = text;
  else delete one[key];
  if (text.trim() && main !== undefined) was[key] = main;
  else delete was[key];
  delete from[key];
  all[lang] = one;
  if (Object.keys(was).length) originals[lang] = was;
  else delete originals[lang];
  if (Object.keys(from).length) sources[lang] = from;
  else delete sources[lang];
  const out: Graph = { ...g, translations: all, translation_originals: originals, translation_sources: sources };
  if (!Object.keys(originals).length) delete out.translation_originals;
  if (!Object.keys(sources).length) delete out.translation_sources;
  return out;
}
