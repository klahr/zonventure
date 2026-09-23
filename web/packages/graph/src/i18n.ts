// Translations of an adventure's text. The graph is written in its main
// language; other languages live in graph.translations, keyed by field —
// the same keys the engine reads (graph.NodeKey, ZoneKey, JournalKey in
// Go). A missing translation falls back to the main text.

import type { Graph, Node } from "./types.ts";

export const nodeKey = (slug: string, field: string) => `n:${slug}:${field}`;
export const zoneKey = (slug: string) => `z:${slug}`;
export const journalKey = (slug: string, field: string) => `j:${slug}:${field}`;
export const TIME_UP_KEY = "time-up";

/** One piece of text an author can translate. */
export interface Translatable {
  key: string;
  /** What the field is, for the form: "Prompt", "Reply 2". */
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
  const p = n.puzzle;
  if (p) {
    add("text", "Prompt", p.prompt, { multiline: true });
    for (const o of p.outputs ?? []) {
      add(`out:${o.port}:label`, `Route “${o.label || o.port}”`, o.label);
      const typed = (o.matchers ?? []).filter((m) => m.kind === "exact" || m.kind === "contains").map((m) => m.value ?? "");
      if (o.trigger === "answer" && typed.length && !p.lock && !p.choices && !p.tiles && !p.search) {
        add(`out:${o.port}:answers`, `Answers for “${o.label || o.port}”`, typed.join("\n"), { multiline: true, answers: true });
      }
    }
    (p.responses ?? []).forEach((r, i) => add(`reply:${i}`, `Wrong-answer reply ${i + 1}`, r.text, { multiline: true }));
    (p.hints ?? []).forEach((h, i) => add(`hint:${i}`, `Hint ${i + 1}`, h.text, { multiline: true }));
    (p.choices?.fields ?? []).forEach((f, i) => {
      add(`field:${i}:label`, `Question ${i + 1}`, f.label);
      for (const o of f.options) add(`field:${i}:opt:${o.value}`, `Question ${i + 1}: option`, o.label || o.value);
    });
    for (const s of p.lock?.symbols ?? []) add(`sym:${s.value}`, "Lock symbol", s.label || (s.image ? "" : s.value));
  }
  if (n.story) add("text", "Text", n.story.text, { multiline: true });
  if (n.end) add("text", "Closing text", n.end.text, { multiline: true });
  if (n.hub) {
    add("hub-text", "Text", n.hub.text, { multiline: true });
    for (const b of n.hub.branches ?? []) {
      add(`branch:${b.port}:label`, `Branch “${b.label || b.port}”`, b.label);
      add(`branch:${b.port}:note`, `Branch “${b.label || b.port}”: subtitle`, b.note);
    }
  }
  return out;
}

/** The adventure-wide texts: journal notes, zone names, the time-up message. */
export function adventureTexts(g: Graph): Translatable[] {
  const out: Translatable[] = [];
  for (const j of g.journal ?? []) {
    if (j.title?.trim()) out.push({ key: journalKey(j.slug, "title"), label: `Journal: ${j.title}`, main: j.title });
    if (j.body?.trim()) out.push({ key: journalKey(j.slug, "body"), label: `Journal: ${j.title || j.slug}`, main: j.body, multiline: true });
  }
  for (const s of new Set((g.journal ?? []).map((j) => j.section).filter((x): x is string => !!x))) {
    // A section is one heading however many notes share it; the key is
    // its first note's, which is what the engine reads for all of them.
    const first = g.journal!.find((j) => j.section === s)!;
    out.push({ key: journalKey(first.slug, "section"), label: "Journal section", main: s });
  }
  for (const z of g.zones ?? []) if (z.name?.trim() && !z.group) out.push({ key: zoneKey(z.slug), label: "Place name", main: z.name });
  if (g.time_limit?.text?.trim()) out.push({ key: TIME_UP_KEY, label: "Time is up: message", main: g.time_limit.text, multiline: true });
  return out;
}

/** Every translatable text of the adventure. */
export function allTexts(g: Graph): Translatable[] {
  return [...g.nodes.flatMap(nodeTexts), ...adventureTexts(g)];
}

/** How much of the adventure is translated into lang, 0–1. */
export function coverage(g: Graph, lang: string): number {
  const all = allTexts(g).filter((t) => !t.answers);
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
