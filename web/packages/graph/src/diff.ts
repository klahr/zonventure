// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// What a draft changes relative to the published version, for the
// "Changes" dialog. Adapted from dazyflow's lib/diffGraphs.ts to adventure
// graphs. Card positions are ignored, as graphstore.Differs ignores them
// on the server — the two must agree, or the toolbar says "unpublished
// changes" while this dialog says "no changes".

import { nodeTexts } from "./i18n.ts";
import type { Edge, Graph, Node } from "./types.ts";

export interface StepChange {
  slug: string;
  title: string;
  /** What changed: "title", "type", "content" (what the step shows) or "settings" (the rest). */
  fields: string[];
  /** Each text that changed, as it was and as it is. */
  texts: TextChange[];
}

/** One of a step's texts in two versions of it. */
export interface TextChange {
  key: string;
  /** What the field is, as nodeTexts labels it: an English key, filled from vars. */
  label: string;
  vars?: Record<string, string | number>;
  before: string;
  after: string;
}

/** The texts that differ between two versions of a step, in the inspector's order; a text one side lacks is empty there. */
export function textChanges(a: Node, b: Node): TextChange[] {
  const was = new Map(nodeTexts(a).map((x) => [x.key, x]));
  const out: TextChange[] = [];
  const push = (x: { key: string; label: string; vars?: Record<string, string | number> }, before: string, after: string) =>
    out.push({ key: x.key, label: x.label, ...(x.vars ? { vars: x.vars } : {}), before, after });
  for (const x of nodeTexts(b)) {
    const before = was.get(x.key)?.main ?? "";
    was.delete(x.key);
    if (before !== x.main) push(x, before, x.main);
  }
  for (const x of was.values()) if (x.main) push(x, x.main, "");
  return out;
}

/** One run of words in a text diff: in both versions, only the old, or only the new. */
export interface DiffPart {
  op: "same" | "del" | "ins";
  text: string;
}

/**
 * The words two texts share and where they differ, whitespace kept, so a
 * one-letter typo fix shows as one word struck and one added. Texts too
 * long to compare word by word come back as all removed and all added.
 */
export function wordDiff(a: string, b: string): DiffPart[] {
  const x = a.match(/\s+|[^\s]+/g) ?? [];
  const y = b.match(/\s+|[^\s]+/g) ?? [];
  if (x.length * y.length > 250_000) {
    return [...(a ? [{ op: "del" as const, text: a }] : []), ...(b ? [{ op: "ins" as const, text: b }] : [])];
  }
  // lcs[i][j]: the longest common run of x[i:] and y[j:].
  const lcs = Array.from({ length: x.length + 1 }, () => new Array<number>(y.length + 1).fill(0));
  for (let i = x.length - 1; i >= 0; i--) {
    for (let j = y.length - 1; j >= 0; j--) lcs[i]![j] = x[i] === y[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
  }
  const out: DiffPart[] = [];
  const add = (op: DiffPart["op"], text: string) => {
    const last = out[out.length - 1];
    if (last?.op === op) last.text += text;
    else out.push({ op, text });
  };
  let i = 0;
  let j = 0;
  while (i < x.length || j < y.length) {
    if (i < x.length && j < y.length && x[i] === y[j]) {
      add("same", x[i]!);
      i++;
      j++;
    } else if (i < x.length && (j === y.length || lcs[i + 1]![j]! >= lcs[i]![j + 1]!)) add("del", x[i++]!);
    else add("ins", y[j++]!);
  }
  return out;
}

export interface GraphDiff {
  added: { slug: string; title: string }[];
  removed: { slug: string; title: string }[];
  changed: StepChange[];
  addedEdges: string[];
  removedEdges: string[];
  /** Adventure-level lists that differ: zones, items, counters, journal. */
  resources: string[];
}

function stable(v: unknown): string {
  return JSON.stringify(v ?? null, (_k, val) => {
    if (val && typeof val === "object" && !Array.isArray(val)) {
      const out: Record<string, unknown> = {};
      for (const k of Object.keys(val as Record<string, unknown>).sort()) out[k] = (val as Record<string, unknown>)[k];
      return out;
    }
    return val;
  });
}

// The node fields whose change counts as the step's settings: its typed
// config, and the journal notes it unlocks.
const SETTINGS = ["puzzle", "story", "hub", "gate", "effect", "switch", "end", "start", "unlocks"] as const;

function stepChanges(a: Node, b: Node): string[] {
  const fields: string[] = [];
  if (a.type !== b.type) fields.push("type");
  if ((a.title ?? "") !== (b.title ?? "")) fields.push("title");
  for (const k of SETTINGS) {
    if (stable(a[k]) === stable(b[k])) continue;
    // A text fix is "content", not a change to how the step works.
    const blocks = (v: unknown) => (v && typeof v === "object" && "blocks" in v ? v.blocks : undefined);
    const rest = (v: unknown) => (v && typeof v === "object" ? { ...v, blocks: undefined } : v);
    if (stable(blocks(a[k])) !== stable(blocks(b[k]))) fields.push("content");
    if (stable(rest(a[k])) !== stable(rest(b[k]))) fields.push("settings");
  }
  return [...new Set(fields)];
}

const wire = (e: Edge) => `${e.from}\0${e.from_port}\0${e.to}`;

const edgeKey = (g: Graph, e: Edge) => {
  const title = (slug: string) => g.nodes.find((n) => n.slug === slug)?.title || slug;
  return `${title(e.from)} · ${e.from_port} → ${title(e.to)}`;
};

export function diffGraphs(baseline: Graph, draft: Graph): GraphDiff {
  const base = new Map(baseline.nodes.map((n) => [n.slug, n]));
  const next = new Map(draft.nodes.map((n) => [n.slug, n]));
  const d: GraphDiff = { added: [], removed: [], changed: [], addedEdges: [], removedEdges: [], resources: [] };
  for (const [slug, n] of next) {
    const b = base.get(slug);
    if (!b) d.added.push({ slug, title: n.title || slug });
    else {
      const fields = stepChanges(b, n);
      if (fields.length) d.changed.push({ slug, title: n.title || slug, fields, texts: textChanges(b, n) });
    }
  }
  for (const [slug, n] of base) if (!next.has(slug)) d.removed.push({ slug, title: n.title || slug });
  const be = new Set(baseline.edges.map(wire));
  const de = new Set(draft.edges.map(wire));
  for (const e of draft.edges) if (!be.has(wire(e))) d.addedEdges.push(edgeKey(draft, e));
  for (const e of baseline.edges) if (!de.has(wire(e))) d.removedEdges.push(edgeKey(baseline, e));
  for (const k of ["zones", "items", "counters", "journal"] as const) {
    if (stable(baseline[k] ?? []) !== stable(draft[k] ?? [])) d.resources.push(k);
  }
  if (!!baseline.journal_by_unlock !== !!draft.journal_by_unlock && !d.resources.includes("journal")) d.resources.push("journal");
  if (stable(baseline.time_limit ?? null) !== stable(draft.time_limit ?? null)) d.resources.push("time_limit");
  if (!!baseline.hide_score !== !!draft.hide_score) d.resources.push("hide_score");
  if ((baseline.language ?? "") !== (draft.language ?? "") || stable(baseline.translations ?? {}) !== stable(draft.translations ?? {}) || stable(baseline.translation_sources ?? {}) !== stable(draft.translation_sources ?? {}))d.resources.push("translations");
  return d;
}

export function diffIsEmpty(d: GraphDiff): boolean {
  return !d.added.length && !d.removed.length && !d.changed.length && !d.addedEdges.length && !d.removedEdges.length && !d.resources.length;
}
