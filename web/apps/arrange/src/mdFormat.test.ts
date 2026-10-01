import { test } from "node:test";
import assert from "node:assert/strict";
import { apply, format, type Format } from "./mdFormat.ts";

/** Format `text` with [ and ] marking the selection; return the result with the new selection marked the same way. */
function run(marked: string, f: Format): string {
  const s = marked.indexOf("[");
  const e = marked.indexOf("]") - 1;
  const text = marked.replace("[", "").replace("]", "");
  const ed = format(text, s, e, f);
  const out = apply(text, ed);
  return out.slice(0, ed.selStart) + "[" + out.slice(ed.selStart, ed.selEnd) + "]" + out.slice(ed.selEnd);
}

test("bold wraps the selection and keeps it selected", () => {
  assert.equal(run("a [word] b", "bold"), "a **[word]** b");
});

test("spaces at the edges stay outside the markers", () => {
  assert.equal(run("a[ word ]b", "bold"), "a **[word]** b");
});

test("bold again takes it off, from inside or with the markers selected", () => {
  assert.equal(run("a **[word]** b", "bold"), "a [word] b");
  assert.equal(run("a [**word**] b", "bold"), "a [word] b");
});

test("italic inside bold adds italic rather than eating a bold star", () => {
  assert.equal(run("**[word]**", "italic"), "***[word]***");
  assert.equal(run("***[word]***", "italic"), "**[word]**");
});

test("nothing selected leaves the caret between the markers", () => {
  assert.equal(run("a [] b", "bold"), "a **[]** b");
});

test("code wraps in backticks", () => {
  assert.equal(run("run [ls]", "code"), "run `[ls]`");
});

test("a link selects the address to type over", () => {
  assert.equal(run("see [the map]", "link"), "see [the map]([https://])");
  assert.equal(run("[https://zv.example]", "link"), "[[]](https://zv.example)");
});

test("lists prefix every line touched, skipping blank ones, and come off again", () => {
  assert.equal(run("[one\ntwo\n\nthree]", "bullets"), "[- one\n- two\n\n- three]");
  assert.equal(run("[- one\n- two]", "bullets"), "[one\ntwo]");
  assert.equal(run("x\n[a\nb]", "numbers"), "x\n[1. a\n2. b]");
});

test("a heading replaces a list marker rather than stacking on it", () => {
  assert.equal(run("- [Title]", "heading"), "[## Title]");
});
