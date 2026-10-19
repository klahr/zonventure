import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// Swedish is the primary market: every string the player can see — this
// app's and @zonventure/play-ui's — needs an entry in sv.json.
const roots = [new URL(".", import.meta.url).pathname, new URL("../../../packages/play-ui/src/", import.meta.url).pathname];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.tsx?$/.test(name) && !name.includes(".test.") ? [p] : [];
  });
}

// Strings that go through t() only as a fallback tone name, never shown.
const IGNORED = new Set(["bad", "good", "info"]);

test("every player string has a Swedish translation", () => {
  const sv = JSON.parse(readFileSync(new URL("./locales/sv.json", import.meta.url), "utf8")) as Record<string, string>;
  const missing = new Set<string>();
  for (const file of roots.flatMap(sources)) {
    const src = readFileSync(file, "utf8");
    for (const m of src.matchAll(/\bt\(\s*"((?:[^"\\]|\\.)*)"/g)) {
      const key = JSON.parse(`"${m[1]}"`) as string;
      if (!(key in sv) && !IGNORED.has(key)) missing.add(key);
    }
  }
  assert.deepEqual([...missing].sort(), []);
});

test("a walk's words change existing strings, in English and Swedish alike", () => {
  const sv = JSON.parse(readFileSync(new URL("./locales/sv.json", import.meta.url), "utf8")) as Record<string, string>;
  const en = JSON.parse(readFileSync(new URL("./locales/en.json", import.meta.url), "utf8")) as Record<string, string>;
  assert.deepEqual(Object.keys(en).filter((k) => !k.startsWith("tour|")), [], "English holds only a walk's words");
  for (const k of [...Object.keys(en), ...Object.keys(sv)].filter((k) => k.startsWith("tour|"))) {
    const base = k.slice("tour|".length);
    assert.ok(base in sv, `${k} changes a string the app does not have`);
    assert.ok(k in en, `${k} has no English`);
  }
});
