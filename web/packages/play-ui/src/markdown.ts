// A deliberately small Markdown subset for puzzle and story text.
//
// Authors write prompts in Markdown; players read them on a phone. The
// output is a tree of plain data that the renderer turns into React
// elements — never an HTML string — so nothing an author types can
// become markup. Legacy rendered author content as HTML, and that was a
// stored-XSS engine; this cannot be one, because there is no path from
// input to innerHTML.
//
// Supported, because prompts use them: paragraphs, line breaks, # to ###
// headings, - and 1. lists, **bold**, *italic* / _italic_, `code`, and
// [links](https://…). Anything else is shown as the text it is.

export type Inline =
  | { t: "text"; v: string }
  | { t: "strong"; c: Inline[] }
  | { t: "em"; c: Inline[] }
  | { t: "code"; v: string }
  | { t: "link"; href: string; c: Inline[] }
  | { t: "br" };

export type Block =
  | { t: "p"; c: Inline[] }
  | { t: "h"; level: 1 | 2 | 3; c: Inline[] }
  | { t: "ul"; items: Inline[][] }
  | { t: "ol"; items: Inline[][] };

/**
 * Only these schemes become links, and a path on this site (/store). A
 * javascript: or data: URL is shown as text instead — a link a player
 * taps must not run anything — and so is //host, /\host or /<tab>/host,
 * which a browser takes for another site.
 */
const SAFE_SCHEMES = ["http:", "https:", "mailto:", "tel:", "geo:"];

export function safeHref(raw: string): string | null {
  const href = raw.trim();
  if (href.startsWith("/")) return /^\/[^/\\\s\x00-\x1f]/.test(href) || href === "/" ? href : null;
  const colon = href.indexOf(":");
  if (colon < 0) return null;
  const scheme = href.slice(0, colon + 1).toLowerCase();
  return SAFE_SCHEMES.includes(scheme) ? href : null;
}

const HEADING = /^(#{1,3})\s+(.*)$/;
const BULLET = /^\s*[-*]\s+(.*)$/;
const NUMBERED = /^\s*\d+[.)]\s+(.*)$/;

export function parseMarkdown(src: string): Block[] {
  const blocks: Block[] = [];
  let para: string[] = [];
  let list: { t: "ul" | "ol"; items: Inline[][] } | null = null;

  const endPara = () => {
    if (para.length === 0) return;
    const c: Inline[] = [];
    para.forEach((line, i) => {
      if (i > 0) c.push({ t: "br" });
      c.push(...parseInline(line));
    });
    blocks.push({ t: "p", c });
    para = [];
  };
  const endList = () => {
    if (list) blocks.push(list);
    list = null;
  };

  for (const raw of src.replace(/\r\n?/g, "\n").split("\n")) {
    const line = raw.trimEnd();
    if (line.trim() === "") {
      endPara();
      endList();
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      endPara();
      endList();
      blocks.push({ t: "h", level: h[1]!.length as 1 | 2 | 3, c: parseInline(h[2]!) });
      continue;
    }
    const b = BULLET.exec(line);
    const n = b ? null : NUMBERED.exec(line);
    if (b || n) {
      endPara();
      const kind = b ? "ul" : "ol";
      if (list && list.t !== kind) endList();
      list ??= { t: kind, items: [] };
      list.items.push(parseInline((b ?? n)![1]!));
      continue;
    }
    endList();
    para.push(line);
  }
  endPara();
  endList();
  return blocks;
}

const ESCAPABLE = "\\*_[]()`#-";

export function parseInline(s: string): Inline[] {
  const out: Inline[] = [];
  let buf = "";
  const flush = () => {
    if (buf) out.push({ t: "text", v: buf });
    buf = "";
  };

  let i = 0;
  while (i < s.length) {
    const ch = s[i]!;

    if (ch === "\\" && i + 1 < s.length && ESCAPABLE.includes(s[i + 1]!)) {
      buf += s[i + 1];
      i += 2;
      continue;
    }

    if (s.startsWith("**", i)) {
      const end = s.indexOf("**", i + 2);
      if (end > i + 2) {
        flush();
        out.push({ t: "strong", c: parseInline(s.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    }

    // Emphasis must open at a word boundary, so snake_case and 2*3*4
    // stay as written.
    if ((ch === "*" || ch === "_") && (i === 0 || /[\s(]/.test(s[i - 1]!)) && s[i + 1] !== " ") {
      const end = s.indexOf(ch, i + 1);
      if (end > i + 1) {
        flush();
        out.push({ t: "em", c: parseInline(s.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }

    if (ch === "`") {
      const end = s.indexOf("`", i + 1);
      if (end > i + 1) {
        flush();
        out.push({ t: "code", v: s.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }

    if (ch === "[") {
      const mid = s.indexOf("](", i + 1);
      const end = mid > i ? s.indexOf(")", mid + 2) : -1;
      const href = end > mid ? safeHref(s.slice(mid + 2, end)) : null;
      if (href) {
        flush();
        out.push({ t: "link", href, c: parseInline(s.slice(i + 1, mid)) });
        i = end + 1;
        continue;
      }
    }

    buf += ch;
    i++;
  }
  flush();
  return out;
}
