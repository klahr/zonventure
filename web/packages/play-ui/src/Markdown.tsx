import { Fragment, useMemo, type ReactNode } from "react";
import { parseMarkdown, type Block, type Inline } from "./markdown.ts";

/** Render author text. See markdown.ts for why this never touches HTML. */
export function Markdown({ text, className }: { text: string | undefined; className?: string }) {
  // An empty field is left out of what the server sends: nothing to show.
  const blocks = useMemo(() => parseMarkdown(text ?? ""), [text]);
  return (
    <div className={className}>
      {blocks.map((b, i) => {
        switch (b.t) {
          case "p":
            return <p key={i}>{inlines(b.c)}</p>;
          case "h": {
            // Author headings sit under the step title, which is the h2.
            const H = (["h3", "h4", "h5"] as const)[b.level - 1]!;
            return <H key={i}>{inlines(b.c)}</H>;
          }
          case "ul":
          case "ol": {
            const L = b.t;
            return (
              <L key={i}>
                {b.items.map((it, j) => (
                  <li key={j}>{inlines(it)}</li>
                ))}
              </L>
            );
          }
        }
      })}
    </div>
  );
}

/** Render author text as one run of inline markup, for a preview inside a button or a line. */
export function MarkdownInline({ text, className }: { text: string | undefined; className?: string }) {
  const nodes = useMemo(() => flatten(parseMarkdown(text ?? "")), [text]);
  return <span className={className}>{inlines(nodes)}</span>;
}

/** Headings, list items and line breaks run together, a space between each. */
function flatten(blocks: Block[]): Inline[] {
  const out: Inline[] = [];
  for (const b of blocks) {
    for (const run of b.t === "ul" || b.t === "ol" ? b.items : [b.c]) {
      if (out.length > 0) out.push({ t: "text", v: " " });
      for (const n of run) out.push(n.t === "br" ? { t: "text", v: " " } : n);
    }
  }
  return out;
}

function inlines(nodes: Inline[]): ReactNode {
  return nodes.map((n, i) => {
    switch (n.t) {
      case "text":
        return <Fragment key={i}>{n.v}</Fragment>;
      case "br":
        return <br key={i} />;
      case "strong":
        return <strong key={i}>{inlines(n.c)}</strong>;
      case "em":
        return <em key={i}>{inlines(n.c)}</em>;
      case "code":
        return <code key={i}>{n.v}</code>;
      case "link":
        return (
          <a key={i} href={n.href} target="_blank" rel="noopener noreferrer">
            {inlines(n.c)}
          </a>
        );
    }
  });
}
