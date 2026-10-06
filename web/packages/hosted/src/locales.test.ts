import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// Swedish is the primary market: every string a host reads
// needs an entry in sv.json.
const dir = new URL(".", import.meta.url).pathname;

test("every hosted string has a Swedish translation", () => {
  const sv = JSON.parse(readFileSync(new URL("./sv.json", import.meta.url), "utf8")) as Record<string, string>;
  const missing = new Set<string>();
  for (const name of readdirSync(dir).filter((n) => /\.tsx?$/.test(n) && !n.includes(".test."))) {
    const src = readFileSync(join(dir, name), "utf8");
    for (const m of src.matchAll(/\bt\(\s*"((?:[^"\\]|\\.)*)"/g)) {
      const key = JSON.parse(`"${m[1]}"`) as string;
      if (!(key in sv)) missing.add(key);
    }
  }
  assert.deepEqual([...missing].sort(), []);
});
