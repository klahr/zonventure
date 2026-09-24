// Content blocks: what a puzzle, story or end shows, in order. See
// internal/graph/blocks.go.

import type { Block, BlockKind, Media, Node } from "./types.ts";

/** The steps that show blocks. */
export function hasBlocks(n: Node): boolean {
  return !!(n.puzzle || n.story || n.end);
}

/** A step's blocks; empty for steps that show none. */
export function blocksOf(n: Node): Block[] {
  return n.puzzle?.blocks ?? n.story?.blocks ?? n.end?.blocks ?? [];
}

/** The step with its blocks replaced. */
export function withBlocks(n: Node, blocks: Block[]): Node {
  const set = <T extends { blocks?: Block[] }>(cfg: T): T => {
    const { blocks: _, ...rest } = cfg;
    return (blocks.length ? { ...rest, blocks } : rest) as T;
  };
  if (n.puzzle) return { ...n, puzzle: set(n.puzzle) };
  if (n.story) return { ...n, story: set(n.story) };
  if (n.end) return { ...n, end: set(n.end) };
  return n;
}

/** The blocks' texts, joined as one Markdown document. */
export function textOf(blocks: Block[] | undefined): string {
  return (blocks ?? [])
    .flatMap((b) => (b.kind === "text" && b.text ? [b.text] : []))
    .join("\n\n");
}

/** The blocks' pictures and sounds, in order. */
export function mediaOf(blocks: Block[] | undefined): Media[] {
  const out: Media[] = [];
  for (const b of blocks ?? []) {
    switch (b.kind) {
      case "image":
      case "audio":
        out.push({ id: b.media, kind: b.kind, ...(b.alt ? { alt: b.alt } : {}) });
    }
  }
  return out;
}

/** An id no block of the step has yet. */
export function freshBlockId(blocks: Block[]): string {
  const taken = new Set(blocks.map((b) => b.id));
  let i = blocks.length + 1;
  while (taken.has(`b${i}`)) i++;
  return `b${i}`;
}

/** A new, empty block of a kind that needs no file. */
export function textBlock(blocks: Block[], text = ""): Block {
  return { id: freshBlockId(blocks), kind: "text", text };
}

/** A block for an uploaded picture or sound. */
export function mediaBlock(blocks: Block[], m: Media): Block {
  return { id: freshBlockId(blocks), kind: m.kind, media: m.id, ...(m.alt ? { alt: m.alt } : {}) };
}

/** The zoom levels a map block may be fixed at: graph.MinZoom, MaxZoom. */
export const MAP_ZOOM = { min: 10, max: 19 } as const;

/** A new compass or map, on the zone given or none yet. */
export function placeBlock(blocks: Block[], kind: "compass" | "map", zone = ""): Block {
  return { id: freshBlockId(blocks), kind, zone };
}

/** The list with the block at `from` moved to `to`. */
export function moveBlock(blocks: Block[], from: number, to: number): Block[] {
  if (from === to || from < 0 || from >= blocks.length || to < 0 || to >= blocks.length) return blocks;
  const out = blocks.slice();
  const [b] = out.splice(from, 1);
  out.splice(to, 0, b!);
  return out;
}

/** Whether a block of this kind carries a file. */
export function isMediaKind(kind: BlockKind): kind is "image" | "audio" {
  return kind === "image" || kind === "audio";
}

type LegacyContent = { prompt?: string; text?: string; media?: Media[]; blocks?: Block[] };

/**
 * A step copied before blocks — from the canvas clipboard, say — with its
 * prompt or text and media as blocks, as the server reads it
 * (graph.LegacyBlocks): the media first, as they were shown, then the text.
 */
export function upgradeNode(n: Node): Node {
  const legacy = (n.puzzle ?? n.story ?? n.end) as LegacyContent | undefined;
  if (!legacy || legacy.blocks) return n;
  const text = legacy.prompt ?? legacy.text ?? "";
  const blocks: Block[] = (legacy.media ?? []).map((m, i) => ({ id: `media-${i + 1}`, kind: m.kind, media: m.id, ...(m.alt ? { alt: m.alt } : {}) }));
  if (text) blocks.push({ id: "text", kind: "text", text });
  const { prompt: _p, text: _t, media: _m, ...rest } = legacy;
  const cfg = { ...rest, ...(blocks.length ? { blocks } : {}) };
  if (n.puzzle) return { ...n, puzzle: cfg as Node["puzzle"] & object };
  if (n.story) return { ...n, story: cfg };
  return { ...n, end: cfg };
}
