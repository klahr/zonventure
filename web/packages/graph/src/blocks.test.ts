import { test } from "node:test";
import assert from "node:assert/strict";
import { blocksOf, freshBlockId, mediaOf, moveBlock, needsFile, newBlock, textOf, upgradeNode, withBlocks } from "./blocks.ts";
import type { Block, Node } from "./types.ts";

const text = (id: string, t: string): Block => ({ id, kind: "text", text: t });

test("a step copied before blocks reads as the server reads it", () => {
  const old = { slug: "q", type: "puzzle", puzzle: { prompt: "Which door?", media: [{ id: 7, kind: "image", alt: "a door" }], zone: "z" } } as unknown as Node;
  assert.deepEqual(upgradeNode(old).puzzle, {
    zone: "z",
    blocks: [{ id: "media-1", kind: "image", media: 7, alt: "a door" }, text("text", "Which door?")],
  });
  const story = { slug: "s", type: "story", story: { text: "" } } as unknown as Node;
  assert.deepEqual(upgradeNode(story).story, {}, "an empty text makes no block");
  const now: Node = { slug: "e", type: "end", end: { blocks: [text("b1", "Done.")] } };
  assert.equal(upgradeNode(now), now, "a step with blocks is left alone");
});

test("blocks move, and a new one gets a free id", () => {
  const bs = [text("b1", "a"), text("b3", "b"), text("b2", "c")];
  assert.deepEqual(moveBlock(bs, 2, 0).map((b) => b.id), ["b2", "b1", "b3"]);
  assert.equal(moveBlock(bs, 0, 5), bs, "out of range is no move");
  assert.equal(freshBlockId(bs), "b4");
  assert.equal(freshBlockId([text("b2", "")]), "b3");
});

test("a step's blocks are read, replaced and flattened", () => {
  const n: Node = { slug: "q", type: "puzzle", puzzle: { outputs: [] } };
  const bs: Block[] = [{ id: "b1", kind: "audio", media: 3 }, text("b2", "One."), { id: "b4", kind: "compass", zone: "z" }, text("b3", "Two.")];
  const m = withBlocks(n, bs);
  assert.deepEqual(blocksOf(m), bs);
  assert.equal(textOf(bs), "One.\n\nTwo.");
  assert.deepEqual(mediaOf(bs), [{ id: 3, kind: "audio" }]);
  assert.deepEqual(withBlocks(m, []).puzzle, { outputs: [] }, "no blocks drops the key");
});

test("the kinds match the shared vectors the server tests", async () => {
  const { readFile } = await import("node:fs/promises");
  const { BLOCK_KINDS } = await import("./blocks.ts");
  const vectors = JSON.parse(await readFile(new URL("../testdata/blocks.json", import.meta.url), "utf8")) as { blocks: Block[] };
  assert.deepEqual([...new Set(vectors.blocks.map((b) => b.kind))].sort(), [...BLOCK_KINDS].sort());
  for (const b of vectors.blocks) if (!needsFile(b.kind)) assert.equal(newBlock([], b.kind).kind, b.kind);
});

test("a step gets its fixed blocks where missing, as the server adds them", async () => {
  const { withFixed } = await import("./blocks.ts");
  const q: Node = { slug: "q", type: "puzzle", puzzle: { blocks: [text("b1", "Look."), { id: "title", kind: "title" }, { id: "points", kind: "image", media: 1 }] } };
  assert.deepEqual(blocksOf(withFixed(q)).map((b) => `${b.id}:${b.kind}`), ["b1:text", "title:title", "points-:points", "points:image"]);
  const again = withFixed(q);
  assert.equal(withFixed(again), again, "a second pass changes nothing");
  const s: Node = { slug: "s", type: "story", story: {} };
  assert.deepEqual(blocksOf(withFixed(s)).map((b) => b.kind), ["title"]);
  const e: Node = { slug: "e", type: "end", end: {} };
  assert.equal(withFixed(e), e, "an end has no fixed blocks");
});
