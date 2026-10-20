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
  // A step saved without its settings — as an assistant may put one —
  // gets them for its type, so the blocks are not silently dropped.
  if (n.puzzle || n.type === "puzzle") return { ...n, puzzle: set(n.puzzle ?? {}) };
  if (n.story || n.type === "story") return { ...n, story: set(n.story ?? {}) };
  if (n.end || n.type === "end") return { ...n, end: set(n.end ?? {}) };
  return n;
}

/** The texts of text and note blocks, joined as one Markdown document. */
export function textOf(blocks: Block[] | undefined): string {
  const out: string[] = [];
  for (const b of blocks ?? []) if ((b.kind === "text" || b.kind === "note") && b.text && !b.disabled) out.push(b.text);
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
      case "call":
        out.push({ id: b.media, kind: "audio", ...(b.alt ? { alt: b.alt } : {}) });
        break;
      case "spotdiff":
        out.push({ id: b.media, kind: "image", ...(b.alt ? { alt: b.alt } : {}) });
        if (b.other) out.push({ id: b.other, kind: "image", ...(b.alt ? { alt: b.alt } : {}) });
        break;
      case "suspects":
        for (const c of b.cards) if (c.media) out.push({ id: c.media, kind: "image", ...(c.alt ? { alt: c.alt } : {}) });
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
export const FILE_KINDS = { image: "image", audio: "audio", video: "video", hotspots: "image", overlay: "image", torch: "image", call: "audio", spotdiff: "image" } as const;
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
  split: { kind: "split", parts: ["", ""] },
  bearing: { kind: "bearing", heading: 0, text: "" },
  suspects: { kind: "suspects", cards: [{ name: "" }] },
  photo: { kind: "photo", text: "", worth: 5 },
  flasher: { kind: "flasher", text: "" },
  letters: { kind: "letters", text: "" },
  shake: { kind: "shake", threshold: 60, text: "" },
  vote: { kind: "vote", text: "", options: ["", ""] },
  bulletin: { kind: "bulletin" },
  draw: { kind: "draw", text: "", options: ["", ""] },
  sabotage: { kind: "sabotage", role: "", text: "", worth: 5 },
  accuse: { kind: "accuse", role: "", text: "", worth: 20 },
  mission: { kind: "mission", role: "", zone: "", count: 2, text: "" },
  roles: { kind: "roles" },
};

/**
 * The blocks a step is given by default, in order, and the only steps
 * that may have them: every puzzle its title and points, every story its
 * title. The author moves them, or removes them. graph.FixedKinds.
 */
export const FIXED_KINDS: Partial<Record<Node["type"], BlockKind[]>> = { puzzle: ["title", "points"], story: ["title"] };

export function isFixed(kind: BlockKind): boolean {
  return kind === "title" || kind === "points";
}

/**
 * The step with any missing fixed block added after the one before it,
 * once, as graph.Normalize does: a step marked fixed keeps what it has.
 */
export function withFixed(n: Node): Node {
  const kinds = FIXED_KINDS[n.type];
  const cfg = n.puzzle ?? n.story;
  if (!kinds || !cfg || cfg.fixed) return n;
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
  const out = withBlocks(n, list);
  return out.puzzle ? { ...out, puzzle: { ...out.puzzle, fixed: true } } : { ...out, story: { ...out.story, fixed: true } };
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

/**
 * The blocks with a block added per file picked for a kind (null: each
 * file as itself). Spot the difference takes two pictures, so a pick of
 * several fills each block's first and other picture in turn.
 */
export function addMediaBlocks(blocks: Block[], files: Media[], kind: FileKind | null): Block[] {
  let next = blocks;
  for (let i = 0; i < files.length; i++) {
    const m = files[i]!;
    // A file stays the kind it was picked for when that kind takes it — a
    // picture for a torch stays a torch, a sound for a call a call.
    const as: FileKind = kind && FILE_KINDS[kind] === m.kind ? kind : m.kind;
    const b = mediaBlock(next, m, as);
    const other = as === "spotdiff" ? files[i + 1] : undefined;
    if (b.kind === "spotdiff" && other?.kind === "image") {
      b.other = other.id;
      i++;
    }
    next = [...next, b];
  }
  return next;
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
