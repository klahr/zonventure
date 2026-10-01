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
  /** What the field is, for the form: "Text", "Reply 2". */
  label: string;
  /** The main-language text, shown beside the translation. */
  main: string;
  multiline?: boolean;
  /** Extra accepted answers, one per line, rather than a translation of text. */
  answers?: boolean;
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
          b.parts.forEach((p, i) => add(`${own}:part:${i}`, `Secret part ${i + 1}`, p, { multiline: true }));
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
        case "sabotage":
        case "accuse":
          add(own, b.kind === "sabotage" ? "Sabotage" : "Accusation", b.text, { multiline: true });
          break;
        case "vote":
        case "draw":
          add(own, b.kind === "vote" ? "Vote question" : "Draw", b.text, { multiline: true });
          b.options.forEach((o, i) => add(`${own}:option:${i}`, b.kind === "vote" ? `Vote option ${i + 1}` : `Card ${i + 1}`, o));
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
          b.options.forEach((o, i) => add(`${own}:option:${i}`, `Quiz answer ${["1", "X", "2"][i]}`, o));
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
      add(`out:${o.port}:label`, `Route “${o.label || o.port}”`, o.label);
      // Only a typed answer is words a team could give in another
      // language; a lock's, a choice's or a scanned code's values are
      // what the phone sends.
      if (o.trigger !== "answer" || p.answer) continue;
      // Each kind of matcher keeps its own key, so a translated answer is
      // matched the way the original is.
      for (const [kind, field, label] of [["exact", "answers", "Answers"], ["contains", "contains", "Words anywhere in the answer"]] as const) {
        const values = (o.matchers ?? []).filter((m) => m.kind === kind).map((m) => m.value ?? "");
        if (values.length) add(`out:${o.port}:${field}`, `${label} for “${o.label || o.port}”`, values.join("\n"), { multiline: true, answers: true });
      }
    }
    add("bar:placeholder", "Answer bar: placeholder", p.bar?.placeholder);
    add("bar:button", "Answer bar: button", p.bar?.button);
    (p.responses ?? []).forEach((r, i) => add(`reply:${i}`, `Wrong-answer reply ${i + 1}`, r.text, { multiline: true }));
    (p.hints ?? []).forEach((h, i) => add(`hint:${i}`, `Hint ${i + 1}`, h.text, { multiline: true }));
    if (p.answer?.kind === "choice") {
      p.answer.fields.forEach((f, i) => {
        add(`field:${i}:label`, `Question ${i + 1}`, f.label);
        for (const o of f.options) add(`field:${i}:opt:${o.value}`, `Question ${i + 1}: option`, o.label || o.value);
      });
    }
    if (p.answer?.kind === "lock") {
      for (const s of p.answer.symbols) add(`sym:${s.value}`, "Lock symbol", s.label || (s.image ? "" : s.value));
    }
    if (p.answer?.kind === "items") {
      p.answer.slots.forEach((s, i) => add(`slot:${i}`, `Slot ${i + 1}`, s.label));
    }
  }
  if (n.story) blocks(n.story.blocks, "Text");
  if (n.end) blocks(n.end.blocks, "Closing text");
  if (n.hub) {
    add("hub-text", "Text", n.hub.text, { multiline: true });
    for (const b of n.hub.branches ?? []) {
      add(`branch:${b.port}:label`, `Branch “${b.label || b.port}”`, b.label);
      add(`branch:${b.port}:note`, `Branch “${b.label || b.port}”: subtitle`, b.note);
    }
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
    if (j.title?.trim()) out.push({ key: journalKey(j.slug, "title"), label: `Journal: ${j.title}`, main: j.title });
    if (j.body?.trim()) out.push({ key: journalKey(j.slug, "body"), label: `Journal: ${j.title || j.slug}`, main: j.body, multiline: true });
  }
  for (const s of new Set((g.journal ?? []).map((j) => j.section).filter((x): x is string => !!x))) {
    out.push({ key: sectionKey(s), label: "Journal section", main: s });
  }
  for (const z of g.zones ?? []) if (z.name?.trim() && !z.group) out.push({ key: zoneKey(z.slug), label: "Place name", main: z.name });
  for (const r of g.roles ?? []) {
    if (r.name?.trim()) out.push({ key: roleKey(r.slug, "name"), label: "Role", main: r.name });
    if (r.text?.trim()) out.push({ key: roleKey(r.slug, "text"), label: `Role: ${r.name || r.slug}`, main: r.text, multiline: true });
    (r.goals ?? []).forEach((g, i) => {
      if (g.text.trim()) out.push({ key: `r:${r.slug}:goal:${i}`, label: `Goal of ${r.name || r.slug}`, main: g.text });
    });
  }
  if (g.time_limit?.text?.trim()) out.push({ key: TIME_UP_KEY, label: "Time is up: message", main: g.time_limit.text, multiline: true });
  return out;
}

/** Every translatable text of the adventure. */
export function allTexts(g: Graph, meta: AdventureMeta = {}): Translatable[] {
  return [...g.nodes.flatMap(nodeTexts), ...adventureTexts(g, meta)];
}

/** How much of the adventure is translated into lang, 0–1. */
export function coverage(g: Graph, lang: string, meta: AdventureMeta = {}): number {
  const all = allTexts(g, meta).filter((t) => !t.answers);
  if (!all.length) return 1;
  const tr = g.translations?.[lang] ?? {};
  return all.filter((t) => tr[t.key]?.trim()).length / all.length;
}

/** The graph with one translation set, or cleared when text is empty. */
export function setTranslation(g: Graph, lang: string, key: string, text: string): Graph {
  const all = { ...(g.translations ?? {}) };
  const one = { ...(all[lang] ?? {}) };
  if (text.trim()) one[key] = text;
  else delete one[key];
  all[lang] = one;
  return { ...g, translations: all };
}
