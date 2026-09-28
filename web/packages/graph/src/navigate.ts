// Moving the selection along the flow from the keyboard: h/j/k/l and the
// arrows in the designer.

import { nodeBySlug, portsOf, targetOf } from "./model.ts";
import type { Graph, Node } from "./types.ts";

/**
 * next is along an output, prev back to a step that leads here, and down
 * and up to the neighbouring branch: the other steps that the same steps
 * lead to, in the order they sit on the canvas.
 */
export type Direction = "next" | "prev" | "down" | "up";

const byPlace = (a: Node, b: Node) => (a.y ?? 0) - (b.y ?? 0) || (a.x ?? 0) - (b.x ?? 0);

/**
 * The step to select when moving from `from` in a direction; undefined
 * when there is none that way. With nothing selected it is the Start.
 */
export function neighbour(g: Graph, from: string | null, dir: Direction): string | undefined {
  const cur = from ? nodeBySlug(g, from) : undefined;
  if (!cur) return g.nodes.find((n) => n.type === "start")?.slug;

  switch (dir) {
    case "next": {
      // The first output that leads somewhere, in port order.
      for (const p of portsOf(cur)) {
        const to = targetOf(g, cur.slug, p);
        if (to && to !== cur.slug) return to;
      }
      return undefined;
    }
    case "prev": {
      // Of the steps that lead here, the one level with it on the canvas.
      const sources = parents(g, cur.slug);
      const y = cur.y ?? 0;
      sources.sort((a, b) => Math.abs((a.y ?? 0) - y) - Math.abs((b.y ?? 0) - y) || (b.x ?? 0) - (a.x ?? 0));
      return sources[0]?.slug;
    }
    case "down":
    case "up": {
      const seen = new Set<string>();
      const siblings: Node[] = [];
      for (const p of parents(g, cur.slug)) {
        for (const e of g.edges) {
          if (e.from !== p.slug || e.to === p.slug || seen.has(e.to)) continue;
          const n = nodeBySlug(g, e.to);
          if (n) {
            seen.add(e.to);
            siblings.push(n);
          }
        }
      }
      siblings.sort(byPlace);
      const i = siblings.findIndex((n) => n.slug === cur.slug);
      return siblings[dir === "down" ? i + 1 : i - 1]?.slug;
    }
  }
}

/** The steps with an edge into `slug`, itself excluded. */
function parents(g: Graph, slug: string): Node[] {
  const out: Node[] = [];
  for (const e of g.edges) {
    if (e.to !== slug || e.from === slug || out.some((n) => n.slug === e.from)) continue;
    const n = nodeBySlug(g, e.from);
    if (n) out.push(n);
  }
  return out;
}
