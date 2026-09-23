// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// What a draft changes relative to the published version, for the
// "Changes" dialog. Adapted from dazyflow's lib/diffGraphs.ts to adventure
// graphs. Card positions are ignored, as graphstore.Differs ignores them
// on the server — the two must agree, or the toolbar says "unpublished
// changes" while this dialog says "no changes".

import type { Edge, Graph, Node } from "./types.ts";

export interface StepChange {
  slug: string;
  title: string;
  /** What changed: "title", "type" or the settings group. */
  fields: string[];
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

const CONFIGS = ["puzzle", "story", "hub", "gate", "effect", "switch", "end", "start", "unlocks"] as const;

function stepChanges(a: Node, b: Node): string[] {
  const fields: string[] = [];
  if (a.type !== b.type) fields.push("type");
  if ((a.title ?? "") !== (b.title ?? "")) fields.push("title");
  for (const k of CONFIGS) if (stable(a[k]) !== stable(b[k])) fields.push("settings");
  return [...new Set(fields)];
}

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
      if (fields.length) d.changed.push({ slug, title: n.title || slug, fields });
    }
  }
  for (const [slug, n] of base) if (!next.has(slug)) d.removed.push({ slug, title: n.title || slug });
  const be = new Set(baseline.edges.map((e) => `${e.from}\0${e.from_port}\0${e.to}`));
  const de = new Set(draft.edges.map((e) => `${e.from}\0${e.from_port}\0${e.to}`));
  for (const e of draft.edges) if (!be.has(`${e.from}\0${e.from_port}\0${e.to}`)) d.addedEdges.push(edgeKey(draft, e));
  for (const e of baseline.edges) if (!de.has(`${e.from}\0${e.from_port}\0${e.to}`)) d.removedEdges.push(edgeKey(baseline, e));
  for (const k of ["zones", "items", "counters", "journal"] as const) {
    if (stable(baseline[k] ?? []) !== stable(draft[k] ?? [])) d.resources.push(k);
  }
  if (!!baseline.journal_by_unlock !== !!draft.journal_by_unlock && !d.resources.includes("journal")) d.resources.push("journal");
  if (stable(baseline.time_limit ?? null) !== stable(draft.time_limit ?? null)) d.resources.push("time_limit");
  if (stable(baseline.translations ?? {}) !== stable(draft.translations ?? {})) d.resources.push("translations");
  return d;
}

export function diffIsEmpty(d: GraphDiff): boolean {
  return !d.added.length && !d.removed.length && !d.changed.length && !d.addedEdges.length && !d.removedEdges.length && !d.resources.length;
}
