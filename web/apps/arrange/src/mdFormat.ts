// Markdown formatting for a text box: what one press of Bold, Italic,
// Link… does to the text and the selection. Pure, so it is tested
// without a DOM; MarkdownArea applies the edit to the textarea.
//
// Only what the player renders (play-ui's markdown.ts) is offered:
// bold, italic, inline code, links, headings and lists.

/** One edit: replace from..to with insert, then select selStart..selEnd. */
export interface Edit {
  from: number;
  to: number;
  insert: string;
  selStart: number;
  selEnd: number;
}

export type Format = "bold" | "italic" | "code" | "link" | "heading" | "bullets" | "numbers";

const MARK: Record<"bold" | "italic" | "code", string> = { bold: "**", italic: "*", code: "`" };

/** The edit a format makes to text with s..e selected. */
export function format(text: string, s: number, e: number, f: Format): Edit {
  switch (f) {
    case "bold":
    case "italic":
    case "code":
      return wrap(text, s, e, MARK[f]);
    case "link":
      return link(text, s, e);
    case "heading":
      return lines(text, s, e, () => "## ", /^#{1,3} /);
    case "bullets":
      return lines(text, s, e, () => "- ", /^[-*] /);
    case "numbers":
      return lines(text, s, e, (i) => `${i + 1}. `, /^\d+\. /);
  }
}

/** Whether `m` sits at i as itself, not as part of a longer run of its character — `*` in `**`. */
function markAt(text: string, i: number, m: string): boolean {
  if (text.slice(i, i + m.length) !== m) return false;
  if (m === "*") {
    // A lone star, or the outer one of `***`: bold and italic together.
    const run = (j: number, step: number) => {
      let n = 0;
      while (text[j] === "*") (n++, (j += step));
      return n;
    };
    const total = run(i, -1) + run(i + 1, 1);
    return total === 1 || total === 3;
  }
  return true;
}

/**
 * Wrap the selection in `m`, or take it off when it is already wrapped —
 * just outside the selection, or as the selection's own ends. Spaces at
 * the selection's edges stay outside: `**word** `, not `**word **`,
 * which Markdown would not read as bold.
 */
function wrap(text: string, s: number, e: number, m: string): Edit {
  const k = m.length;
  // Already wrapped, just outside the selection: unwrap.
  if (s >= k && markAt(text, s - k, m) && markAt(text, e, m)) {
    return { from: s - k, to: e + k, insert: text.slice(s, e), selStart: s - k, selEnd: e - k };
  }
  const sel = text.slice(s, e);
  // The selection includes its markers: unwrap them.
  if (sel.length >= 2 * k && sel.startsWith(m) && sel.endsWith(m) && markAt(text, s, m) && markAt(text, e - k, m)) {
    const inner = sel.slice(k, sel.length - k);
    return { from: s, to: e, insert: inner, selStart: s, selEnd: s + inner.length };
  }
  const lead = sel.length - sel.trimStart().length;
  const trail = sel.length - sel.trimEnd().length;
  const core = sel.trim();
  if (!core) {
    // Nothing chosen: markers with the caret between, to type into.
    return { from: s, to: e, insert: sel + m + m, selStart: e + k, selEnd: e + k };
  }
  const insert = sel.slice(0, lead) + m + core + m + sel.slice(sel.length - trail);
  return { from: s, to: e, insert, selStart: s + lead + k, selEnd: s + lead + k + core.length };
}

/**
 * Make the selection a link. Selected words become its text, with the
 * address left selected to type over; a selected address becomes the
 * address, with the caret where its text goes.
 */
function link(text: string, s: number, e: number): Edit {
  const sel = text.slice(s, e).trim();
  if (/^https?:\/\/\S+$/.test(sel)) {
    return { from: s, to: e, insert: `[](${sel})`, selStart: s + 1, selEnd: s + 1 };
  }
  const label = sel || "link";
  const insert = `[${label}](https://)`;
  const at = s + label.length + 3;
  return { from: s, to: e, insert, selStart: at, selEnd: at + "https://".length };
}

/**
 * Prefix every line the selection touches — or take the prefix off when
 * every one has it. Blank lines are left alone, so a list does not get
 * empty items.
 */
function lines(text: string, s: number, e: number, prefix: (i: number) => string, has: RegExp): Edit {
  const from = text.lastIndexOf("\n", s - 1) + 1;
  let to = text.indexOf("\n", e > s && text[e - 1] === "\n" ? e - 1 : e);
  if (to < 0) to = text.length;
  const ls = text.slice(from, to).split("\n");
  const filled = ls.filter((l) => l.trim());
  const off = filled.length > 0 && filled.every((l) => has.test(l));
  let n = 0;
  const out = ls.map((l) => {
    if (!l.trim()) return l;
    const bare = l.replace(/^#{1,3} |^[-*] |^\d+\. /, "");
    return off ? bare : prefix(n++) + bare;
  });
  const insert = out.join("\n");
  return { from, to, insert, selStart: from, selEnd: from + insert.length };
}

/** Apply an edit to a string: for tests, and for a textarea without execCommand. */
export function apply(text: string, ed: Edit): string {
  return text.slice(0, ed.from) + ed.insert + text.slice(ed.to);
}
