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
  return g.frames?.length ? tidyFramed(g, sizes) : tidyAll(g, sizes);
}

const DEFAULT_SIZE: Size = { w: 240, h: 110 };
const FRAME_PAD = 50;
const FRAME_HEAD = 40;
const FRAME_GAP = 60;

/**
 * Tidy an adventure the author has grouped: the steps inside each frame
 * are laid out within it and the frame fits itself to them, so the
 * grouping survives. Frames keep their place and order; one that grows
 * pushes those below it down, so none overlap. Steps outside every frame
 * are left where they are.
 */
function tidyFramed(g: Graph, sizes: Map<string, Size>): Graph {
  const size = (slug: string) => sizes.get(slug) ?? DEFAULT_SIZE;
  const frames = [...g.frames!].sort((a, b) => a.y - b.y || a.x - b.x);
  const owner = new Map<string, string>();
  for (const n of g.nodes) {
    const s = size(n.slug);
    const cx = (n.x ?? 0) + s.w / 2;
    const cy = (n.y ?? 0) + s.h / 2;
    const f = frames.find((f) => cx >= f.x && cx <= f.x + f.width && cy >= f.y && cy <= f.y + f.height);
    if (f) owner.set(n.slug, f.id);
  }

  const moved = new Map<string, { x: number; y: number }>();
  const fitted = new Map<string, { x: number; y: number; width: number; height: number }>();
  let shift = 0;
  let lastBottom = -Infinity;
  for (const f of frames) {
    const members = g.nodes.filter((n) => owner.get(n.slug) === f.id);
    const y = Math.max(f.y + shift, lastBottom === -Infinity ? -Infinity : lastBottom + FRAME_GAP);
    if (!members.length) {
      fitted.set(f.id, { x: f.x, y, width: f.width, height: f.height });
      lastBottom = y + f.height;
      continue;
    }
    const inside = new Set(members.map((n) => n.slug));
    const sub = tidyAll({ nodes: members, edges: g.edges.filter((e) => inside.has(e.from) && inside.has(e.to)) }, sizes);
    const xs = sub.nodes.map((n) => n.x ?? 0);
    const ys = sub.nodes.map((n) => n.y ?? 0);
    const minX = Math.min(...xs);
    const minY = Math.min(...ys);
    let right = 0;
    let bottom = 0;
    for (const n of sub.nodes) {
      const s = size(n.slug);
      const px = f.x + FRAME_PAD + (n.x ?? 0) - minX;
      const py = y + FRAME_PAD + FRAME_HEAD + (n.y ?? 0) - minY;
      moved.set(n.slug, { x: px, y: py });
      right = Math.max(right, px + s.w);
      bottom = Math.max(bottom, py + s.h);
    }
    const height = bottom + FRAME_PAD - y;
    fitted.set(f.id, { x: f.x, y, width: right + FRAME_PAD - f.x, height });
    shift = y - f.y;
    lastBottom = y + height;
  }

  return {
    ...g,
    nodes: g.nodes.map((n) => {
      const p = moved.get(n.slug);
      return p ? { ...n, x: Math.round(p.x), y: Math.round(p.y) } : n;
    }),
    frames: g.frames!.map((f) => {
      const r = fitted.get(f.id);
      return r ? { ...f, x: Math.round(r.x), y: Math.round(r.y), width: Math.round(r.width), height: Math.round(r.height) } : f;
    }),
  };
}

function tidyAll(g: Graph, sizes: Map<string, Size>): Graph {
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
    const s = sizes.get(n.slug) ?? DEFAULT_SIZE;
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
