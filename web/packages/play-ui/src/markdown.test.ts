import { test } from "node:test";
import assert from "node:assert/strict";
import { parseInline, parseMarkdown, safeHref } from "./markdown.ts";

test("paragraphs split on blank lines and keep line breaks", () => {
  assert.deepEqual(parseMarkdown("one\ntwo\n\nthree"), [
    { t: "p", c: [{ t: "text", v: "one" }, { t: "br" }, { t: "text", v: "two" }] },
    { t: "p", c: [{ t: "text", v: "three" }] },
  ]);
});

test("headings and lists", () => {
  assert.deepEqual(parseMarkdown("## Clue\n- red\n- blue\n1. first"), [
    { t: "h", level: 2, c: [{ t: "text", v: "Clue" }] },
    { t: "ul", items: [[{ t: "text", v: "red" }], [{ t: "text", v: "blue" }]] },
    { t: "ol", items: [[{ t: "text", v: "first" }]] },
  ]);
});

test("inline emphasis, code and links", () => {
  assert.deepEqual(parseInline("**Look** *up* at `12:00` [map](https://osm.org)"), [
    { t: "strong", c: [{ t: "text", v: "Look" }] },
    { t: "text", v: " " },
    { t: "em", c: [{ t: "text", v: "up" }] },
    { t: "text", v: " at " },
    { t: "code", v: "12:00" },
    { t: "text", v: " " },
    { t: "link", href: "https://osm.org", c: [{ t: "text", v: "map" }] },
  ]);
});

test("word-internal markers stay literal", () => {
  assert.deepEqual(parseInline("snake_case_name and 2*3*4"), [{ t: "text", v: "snake_case_name and 2*3*4" }]);
});

test("an unclosed marker is just text", () => {
  assert.deepEqual(parseInline("**almost"), [{ t: "text", v: "**almost" }]);
});

test("escapes", () => {
  assert.deepEqual(parseInline("\\*not em\\*"), [{ t: "text", v: "*not em*" }]);
});

test("unsafe links are shown as text, never as links", () => {
  for (const href of ["javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,x", "vbscript:x", "/relative"]) {
    assert.equal(safeHref(href), null, href);
    const out = parseInline(`[tap](${href})`);
    assert.ok(out.every((n) => n.t === "text"), `${href} became ${JSON.stringify(out)}`);
  }
  assert.equal(safeHref("geo:59.3,18.0"), "geo:59.3,18.0");
});
