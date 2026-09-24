import { test } from "node:test";
import assert from "node:assert/strict";
import { blocksOf, freshBlockId, mediaOf, moveBlock, textOf, upgradeNode, withBlocks } from "./blocks.ts";
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
