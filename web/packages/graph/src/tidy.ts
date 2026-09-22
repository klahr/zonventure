// "Tidy": lay the graph out left to right by flow, using the column
// assignment and packing from layout.ts (copied from dazyflow).

import { layerNodes, packColumns } from "./layout.ts";
import type { Graph } from "./types.ts";

export interface Size {
  w: number;
  h: number;
}

/**
 * Return the graph with every node repositioned. `sizes` are the measured
 * card sizes by slug; unmeasured cards fall back to a default so a graph
 * can be tidied before the canvas has rendered it.
 *
 * Start is treated as the entry point, so it anchors the left edge even
 * when its only wire runs to something far along.
 */
export function tidy(g: Graph, sizes: Map<string, Size> = new Map()): Graph {
  const fallback: Size = { w: 240, h: 110 };
  const ids = g.nodes.map((n) => n.slug);

  // Edges back into a hub are what make a hub loop; laying them out
  // would drag the hub to the right of its own branches. Leave them out
  // of the column assignment — they are drawn, just not ranked.
  const hubs = new Set(g.nodes.filter((n) => n.type === "hub").map((n) => n.slug));
  const forward = g.edges
    .filter((e) => !hubs.has(e.to) || !reaches(g, e.to, e.from))
    .map((e) => ({ source: e.from, target: e.to }));

  const starts = new Set(g.nodes.filter((n) => n.type === "start").map((n) => n.slug));
  const layer = layerNodes(ids, forward, (id) => starts.has(id));
  const boxes = g.nodes.map((n) => {
    const s = sizes.get(n.slug) ?? fallback;
    return { id: n.slug, x: n.x ?? 0, y: n.y ?? 0, w: s.w, h: s.h };
  });
  const pos = packColumns(boxes, layer, { hgap: 96, vgap: 40, startX: 0, startY: 0 });

  return {
    ...g,
    nodes: g.nodes.map((n) => {
      const p = pos.get(n.slug);
      return p ? { ...n, x: Math.round(p.x), y: Math.round(p.y) } : n;
    }),
  };
}

/** Whether `to` is reachable from `from` by following edges. */
function reaches(g: Graph, from: string, to: string): boolean {
  const seen = new Set<string>([from]);
  const stack = [from];
  while (stack.length) {
    const cur = stack.pop()!;
    if (cur === to) return true;
    for (const e of g.edges) {
      if (e.from === cur && !seen.has(e.to)) {
        seen.add(e.to);
        stack.push(e.to);
      }
    }
  }
  return false;
}
