import type { Editor } from "./types.ts";
import { withField } from "./types.ts";
import { MarkdownArea } from "../MarkdownArea.tsx";

export const TextEditor: Editor<"text"> = ({ block, set, t, textLabel, graph, node }) => (
  <>
    <MarkdownArea
      className="field__input"
      rows={4}
      value={block.text}
      placeholder={t.t("What the team reads. **Bold** and *italic* work.")}
      aria-label={textLabel}
      onChange={(e) => set({ ...block, text: e.target.value })}
    />
    {((graph.items?.length ?? 0) > 0 || (graph.counters?.length ?? 0) > 0) && (
      // Picked by name, written by slug: {{item:…}} keeps working when an item is renamed.
      <select
        className="field__input"
        value=""
        aria-label={t.t("Show a value in the text")}
        onChange={(e) => e.target.value && set({ ...block, text: (block.text ? block.text.replace(/\s*$/, " ") : "") + e.target.value })}
      >
        <option value="">{t.t("Show a value in the text…")}</option>
        {(graph.items ?? []).map((i) => (
          <option key={"i" + i.slug} value={`{{item:${i.slug}}}`}>
            {t.t("How many {{name}} the team has", { name: i.name || i.slug })}
          </option>
        ))}
        {(graph.counters ?? []).map((c) => (
          <option key={"c" + c.slug} value={`{{counter:${c.slug}}}`}>
            {t.t("Counter: {{name}}", { name: c.name || c.slug })}
          </option>
        ))}
      </select>
    )}
    {/* A party host rewrites a step's words; an item's (not a step) are the author's. */}
    {graph.nodes.some((n) => n.slug === node.slug) && (
      <label className="check" title={t.t("At a children's party, the host can put their own words here. Everyone else reads yours.")}>
        <input type="checkbox" checked={!!block.host_edit} onChange={(e) => set(withField(block, "host_edit", e.target.checked || undefined))} />
        {t.t("A party host may rewrite it")}
      </label>
    )}
  </>
);

/** Where the title goes; the title itself is edited at the top of the inspector. */
export const TitleEditor: Editor<"title"> = ({ node, t }) => (
  <p className="field__hint">{node.title ? t.t("Shows “{{title}}”. Change it at the top of the inspector.", { title: node.title }) : t.t("Shows the step's title, once it has one.")}</p>
);

/** Where the points show; they come from the answer routes. */
export const PointsEditor: Editor<"points"> = ({ node, t }) => {
  const best = Math.max(0, ...(node.puzzle?.outputs ?? []).filter((o) => o.trigger === "answer").map((o) => o.points ?? 0));
  return (
    <p className="field__hint">{best ? t.t("Shows “Worth {{n}} points”, the best answer's points.", { n: best }) : t.t("Shows nothing until an answer route gives points.")}</p>
  );
};
