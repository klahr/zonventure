// Content blocks: what a puzzle, story or end shows, in order. See
// internal/graph/blocks.go.

import type { Block, BlockBody, BlockKind, Media, Node } from "./types.ts";

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

/** The texts of text and note blocks, joined as one Markdown document. */
export function textOf(blocks: Block[] | undefined): string {
  const out: string[] = [];
  for (const b of blocks ?? []) if ((b.kind === "text" || b.kind === "note") && b.text) out.push(b.text);
  return out.join("\n\n");
}

/** The blocks' files, in order: graph.MediaOf. */
export function mediaOf(blocks: Block[] | undefined): Media[] {
  const out: Media[] = [];
  for (const b of blocks ?? []) {
    switch (b.kind) {
      case "image":
      case "audio":
      case "video":
        out.push({ id: b.media, kind: b.kind, ...(b.alt ? { alt: b.alt } : {}) });
        break;
      case "hotspots":
      case "overlay":
      case "torch":
        out.push({ id: b.media, kind: "image", ...(b.alt ? { alt: b.alt } : {}) });
        break;
      case "chat":
        if (b.media) out.push({ id: b.media, kind: "image", ...(b.alt ? { alt: b.alt } : {}) });
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

/** The kinds whose block needs a file, and which. */
export const FILE_KINDS = { image: "image", audio: "audio", video: "video", hotspots: "image", overlay: "image", torch: "image" } as const;
export type FileKind = keyof typeof FILE_KINDS;

export function needsFile(kind: BlockKind): kind is FileKind {
  return kind in FILE_KINDS;
}

/** The fields a new block of a kind that needs no file starts with. */
const EMPTY: { [K in Exclude<BlockKind, FileKind>]: Extract<BlockBody, { kind: K }> } = {
  text: { kind: "text", text: "" },
  compass: { kind: "compass", zone: "" },
  map: { kind: "map", zone: "" },
  note: { kind: "note", text: "", style: "paper" },
  chat: { kind: "chat", lines: [{ who: "", text: "" }] },
  cipher: { kind: "cipher", cipher: "morse" },
  divider: { kind: "divider", style: "line" },
  radar: { kind: "radar", zone: "" },
  level: { kind: "level", pitch: 0 },
  noise: { kind: "noise", threshold: 70 },
  countdown: { kind: "countdown", seconds: 300 },
  status: { kind: "status", items: true },
  standings: { kind: "standings" },
  title: { kind: "title" },
  points: { kind: "points" },
  quiz: { kind: "quiz", text: "", options: ["", "", ""] },
  tiebreak: { kind: "tiebreak", text: "" },
};

/**
 * The blocks a step always has, in order: every puzzle its title and
 * points, every story its title. The author moves them, never removes
 * them. graph.FixedKinds.
 */
export const FIXED_KINDS: Partial<Record<Node["type"], BlockKind[]>> = { puzzle: ["title", "points"], story: ["title"] };

export function isFixed(kind: BlockKind): boolean {
  return kind === "title" || kind === "points";
}

/** The step with any missing fixed block added after the one before it, as graph.Normalize does. */
export function withFixed(n: Node): Node {
  const kinds = FIXED_KINDS[n.type];
  if (!kinds || !hasBlocks(n)) return n;
  let list = blocksOf(n);
  let at = 0;
  for (const k of kinds) {
    const i = list.findIndex((b) => b.kind === k);
    if (i >= 0) {
      at = i + 1;
      continue;
    }
    let id: string = k;
    while (list.some((b) => b.id === id)) id += "-";
    list = [...list.slice(0, at), { id, kind: k } as Block, ...list.slice(at)];
    at++;
  }
  return list === blocksOf(n) ? n : withBlocks(n, list);
}

/** Every kind, for the shared test vectors (graph.BlockKinds). */
export const BLOCK_KINDS = [...Object.keys(EMPTY), ...Object.keys(FILE_KINDS)] as BlockKind[];

/** A new, empty block of a kind that needs no file. */
export function newBlock(blocks: Block[], kind: Exclude<BlockKind, FileKind>): Block {
  return { id: freshBlockId(blocks), ...structuredClone(EMPTY[kind]) } as Block;
}

/** A new, empty text block. */
export function textBlock(blocks: Block[], text = ""): Block {
  return { id: freshBlockId(blocks), kind: "text", text };
}

/** A block for an uploaded file: a picture, sound or film as itself, or a picture for a kind that uses one. */
export function mediaBlock(blocks: Block[], m: Media, kind: FileKind = m.kind): Block {
  return { id: freshBlockId(blocks), kind, media: m.id, ...(m.alt ? { alt: m.alt } : {}) } as Block;
}

/** A new compass or map, on the zone given or none yet. */
export function placeBlock(blocks: Block[], kind: "compass" | "map", zone = ""): Block {
  return { id: freshBlockId(blocks), kind, zone };
}

/** The zoom levels a map block may be fixed at: graph.MinZoom, MaxZoom. */
export const MAP_ZOOM = { min: 10, max: 19 } as const;

/** The list with the block at `from` moved to `to`. */
export function moveBlock(blocks: Block[], from: number, to: number): Block[] {
  if (from === to || from < 0 || from >= blocks.length || to < 0 || to >= blocks.length) return blocks;
  const out = blocks.slice();
  const [b] = out.splice(from, 1);
  out.splice(to, 0, b!);
  return out;
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
