import { QUIZ_CHOICES, type QuizChoice } from "@zonventure/graph";
import type { Editor } from "./types.ts";
import { withField } from "./types.ts";
import { MarkdownArea } from "../MarkdownArea.tsx";

/** A quiz-walk question: the question, its three answers, which is right, and its worth. */
export const QuizEditor: Editor<"quiz"> = ({ block, set, t }) => (
  <>
    <MarkdownArea className="field__input" rows={2} value={block.text} placeholder={t.t("The question")} aria-label={t.t("Question")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <div className="quiz-ed" role="radiogroup" aria-label={t.t("Right answer")}>
      {QUIZ_CHOICES.map((c, i) => (
        <label key={c} className={"quiz-ed__row" + (block.answer === c ? " is-right" : "")}>
          <input type="radio" name={`right-${block.id}`} checked={block.answer === c} onChange={() => set({ ...block, answer: c as QuizChoice })} aria-label={t.t("{{choice}} is right", { choice: c })} />
          <span className="quiz-ed__mark">{c}</span>
          <input
            className="field__input"
            value={block.options[i]}
            placeholder={t.t("Answer {{choice}}", { choice: c })}
            onChange={(e) => {
              const options = [...block.options] as [string, string, string];
              options[i] = e.target.value;
              set({ ...block, options });
            }}
          />
        </label>
      ))}
    </div>
    <label className="field field--num">
      <span className="field__label">{t.t("Points when right")}</span>
      <input className="field__input" type="number" min={1} value={block.worth || 1} onChange={(e) => set(withField(block, "worth", Math.max(1, Number(e.target.value) || 1) === 1 ? undefined : Math.max(1, Number(e.target.value))))} />
    </label>
    <p className="field__hint">{t.t("Tick the right answer. Teams see whether they were right, and get the points, when they finish.")}</p>
  </>
);

/** The tie-breaker: a question with a number for an answer. */
export const TiebreakEditor: Editor<"tiebreak"> = ({ block, set, t }) => (
  <>
    <MarkdownArea className="field__input" rows={2} value={block.text} placeholder={t.t("The question, such as “How many steps up the tower?”")} aria-label={t.t("Question")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <label className="field field--num">
      <span className="field__label">{t.t("Right number")}</span>
      <input
        className="field__input"
        type="number"
        step="any"
        value={block.target ?? ""}
        onChange={(e) => set(withField(block, "target", e.target.value === "" ? undefined : Number(e.target.value)))}
      />
    </label>
    <p className="field__hint">{t.t("Teams with the same score are ranked by how close their guess was. One per adventure.")}</p>
  </>
);
