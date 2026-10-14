import type { Edge, Graph } from "./types.ts";

/**
 * A three-way merge of two drafts that both started from `base`: the
 * author's own (`mine`) and one someone else saved meanwhile (`theirs`).
 *
 * Collections merge item by item — steps by slug, wires by their port,
 * zones, items, counters, notes and frames by id — and a step merges
 * field by field, so two people working on different steps, or on
 * different parts of one, never clash. Where both changed the same thing
 * differently, the author's own edit is kept and the clash is reported,
 * so the designer can say which steps to look over — or, with `prefer`
 * "theirs", theirs is. Positions are kept
 * the same way but never reported: a card moved twice is not a conflict.
 */
export interface MergeResult {
  graph: Graph;
  /** Steps (or, for resources, "zone:slug" and the like) both changed. */
  conflicts: string[];
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** Whose edit wins where both changed the same thing. */
export type MergePrefer = "mine" | "theirs";

/** The value both sides agree on, or else the side that changed it. */
function pick<T>(b: T, m: T, t: T, prefer: MergePrefer): { value: T; clash: boolean } {
  if (same(m, t) || same(t, b)) return { value: m, clash: false };
  if (same(m, b)) return { value: t, clash: false };
  return { value: prefer === "theirs" ? t : m, clash: true };
}

const COSMETIC = new Set(["x", "y"]);

/** Merges two records field by field; returns the fields that clashed. */
function mergeFields<T extends object>(b: T | undefined, m: T, t: T, prefer: MergePrefer): { value: T; clashes: string[] } {
  const bb = (b ?? {}) as Record<string, unknown>;
  const mm = m as Record<string, unknown>;
  const tt = t as Record<string, unknown>;
  const out: Record<string, unknown> = {};
  const clashes: string[] = [];
  for (const k of new Set([...Object.keys(mm), ...Object.keys(tt), ...Object.keys(bb)])) {
    const r = pick(bb[k], mm[k], tt[k], prefer);
    if (r.clash && !COSMETIC.has(k)) clashes.push(k);
    if (r.value !== undefined) out[k] = r.value;
  }
  return { value: out as T, clashes };
}

/**
 * Merges a keyed list. Order follows theirs, with what only the author
 * added after it in the author's order. `fieldwise` merges an item both
 * changed field by field instead of keeping the author's whole.
 */
function mergeList<T>(
  b: T[] | undefined,
  m: T[] | undefined,
  t: T[] | undefined,
  key: (x: T) => string,
  fieldwise: boolean,
  clash: (k: string) => void,
  prefer: MergePrefer,
): T[] | undefined {
  if (b === undefined && m === undefined && t === undefined) return undefined;
  const index = (xs: T[] | undefined) => new Map((xs ?? []).map((x) => [key(x), x]));
  const bm = index(b);
  const mm = index(m);
  const tm = index(t);
  const order = [...(t ?? []).map(key), ...(m ?? []).map(key).filter((k) => !tm.has(k))];
  const out: T[] = [];
  for (const k of order) {
    const bi = bm.get(k);
    const mi = mm.get(k);
    const ti = tm.get(k);
    if (mi !== undefined && ti !== undefined && fieldwise && !same(mi, ti)) {
      const r = mergeFields(bi as object | undefined, mi as object, ti as object, prefer);
      if (r.clashes.length) clash(k);
      out.push(r.value as T);
      continue;
    }
    const r = pick(bi, mi, ti, prefer);
    if (r.clash) clash(k);
    if (r.value !== undefined) out.push(r.value);
  }
  // A list that was absent stays absent when merging leaves it empty.
  if (!out.length && !m?.length && !t?.length && (m === undefined || t === undefined)) return m ?? t;
  return out;
}

const KEYED = ["nodes", "edges", "zones", "items", "counters", "journal", "frames", "translations", "translation_originals", "translation_sources"];

/** A graph's settings: everything but its keyed collections. */
const scalars = (g: Graph): Record<string, unknown> =>
  Object.fromEntries(Object.entries(g).filter(([k]) => !KEYED.includes(k)));

export function mergeGraphs(base: Graph, mine: Graph, theirs: Graph, prefer: MergePrefer = "mine"): MergeResult {
  const conflicts = new Set<string>();
  const as = (prefix: string) => (k: string) => conflicts.add(prefix + k);
  const edgeKey = (e: Edge) => `${e.from}\0${e.from_port}`;

  const nodes = mergeList(base.nodes, mine.nodes, theirs.nodes, (n) => n.slug, true, as(""), prefer) ?? [];
  const slugs = new Set(nodes.map((n) => n.slug));
  // A wire clash is the step it leaves from; a wire to or from a step
  // the other side deleted goes with it.
  const edges = (
    mergeList(base.edges, mine.edges, theirs.edges, edgeKey, false, (k) => conflicts.add(k.split("\0")[0]!), prefer) ?? []
  ).filter((e) => slugs.has(e.from) && slugs.has(e.to));

  const rest = mergeFields(scalars(base), scalars(mine), scalars(theirs), prefer);
  for (const k of rest.clashes) conflicts.add("graph:" + k);

  const graph = { ...rest.value, nodes, edges } as Graph;
  const lists = {
    zones: mergeList(base.zones, mine.zones, theirs.zones, (z) => z.slug, false, as("zone:"), prefer),
    items: mergeList(base.items, mine.items, theirs.items, (i) => i.slug, false, as("item:"), prefer),
    counters: mergeList(base.counters, mine.counters, theirs.counters, (c) => c.slug, false, as("counter:"), prefer),
    journal: mergeList(base.journal, mine.journal, theirs.journal, (j) => j.slug, false, as("note:"), prefer),
    frames: mergeList(base.frames, mine.frames, theirs.frames, (f) => f.id, false, as("frame:"), prefer),
  };
  for (const [k, v] of Object.entries(lists)) if (v !== undefined) (graph as unknown as Record<string, unknown>)[k] = v;

  // Translations merge text by text, and what each was made from with them.
  for (const field of ["translations", "translation_originals", "translation_sources"] as const) {
    if (!base[field] && !mine[field] && !theirs[field]) continue;
    const out: Record<string, Record<string, string>> = {};
    const langs = new Set([...Object.keys(base[field] ?? {}), ...Object.keys(mine[field] ?? {}), ...Object.keys(theirs[field] ?? {})]);
    for (const lang of langs) {
      const r = mergeFields(base[field]?.[lang], mine[field]?.[lang] ?? {}, theirs[field]?.[lang] ?? {}, prefer);
      if (field === "translations") for (const k of r.clashes) conflicts.add(`translation:${lang}:${k}`);
      const deletedByOne = base[field]?.[lang] && (!mine[field]?.[lang] || !theirs[field]?.[lang]) && !Object.keys(r.value).length;
      if (!deletedByOne) out[lang] = r.value;
    }
    graph[field] = out;
  }
  return { graph, conflicts: [...conflicts] };
}
