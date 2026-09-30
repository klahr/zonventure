import type { Editor } from "./types.ts";
import { MarkdownArea } from "../MarkdownArea.tsx";

export const TextEditor: Editor<"text"> = ({ block, set, t, textLabel }) => (
  <MarkdownArea
    className="field__input"
    rows={4}
    value={block.text}
    placeholder={t.t("What the team reads. **Bold** and *italic* work.")}
    aria-label={textLabel}
    onChange={(e) => set({ ...block, text: e.target.value })}
  />
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
