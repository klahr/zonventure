import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// Every string the console shows needs Swedish: from the classic
// console's catalog, an area file under locales/sv/, or sv.json — the
// same three layers i18n.tsx merges.
const src = new URL(".", import.meta.url).pathname;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === "locales" ? [] : sources(p);
    return /\.tsx?$/.test(name) && !name.includes(".test.") ? [p] : [];
  });
}

function catalog(): Record<string, string> {
  const read = (p: string) => JSON.parse(readFileSync(p, "utf8")) as Record<string, string>;
  const areas = join(src, "locales/sv");
  return Object.assign(
    read(new URL("../../../../internal/server/web/locales/web/sv.json", import.meta.url).pathname),
    ...readdirSync(areas).filter((f) => f.endsWith(".json")).map((f) => read(join(areas, f))),
    read(join(src, "locales/sv.json")),
  );
}

test("every console string has a Swedish translation", () => {
  const sv = catalog();
  const missing = new Set<string>();
  for (const file of sources(src)) {
    for (const m of readFileSync(file, "utf8").matchAll(/\bt\.t\(\s*"((?:[^"\\]|\\.)*)"/g)) {
      const key = JSON.parse(`"${m[1]}"`) as string;
      if (!(key in sv)) missing.add(key);
    }
  }
  assert.deepEqual([...missing].sort(), []);
});
