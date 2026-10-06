import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// Swedish is the primary market: every string the creator shows needs
// an entry in sv.json — t("…") calls, and the template and kind labels.
const dir = new URL(".", import.meta.url).pathname;

test("every creator string has a Swedish translation", () => {
  const sv = JSON.parse(readFileSync(new URL("./sv.json", import.meta.url), "utf8")) as Record<string, string>;
  const missing = new Set<string>();
  for (const name of readdirSync(dir).filter((n) => /\.tsx?$/.test(n) && !n.includes(".test."))) {
    const src = readFileSync(join(dir, name), "utf8");
    for (const m of src.matchAll(/\bt\(\s*"((?:[^"\\]|\\.)*)"|(?:label|blurb): "((?:[^"\\]|\\.)*)"/g)) {
      const key = JSON.parse(`"${m[1] ?? m[2]}"`) as string;
      if (!(key in sv)) missing.add(key);
    }
  }
  assert.deepEqual([...missing].sort(), []);
});
