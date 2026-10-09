import { useState } from "react";
import type { play } from "@zonventure/api";
import type { Translate } from "../feedback.ts";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./quiz.css";

const CHOICES = ["1", "X", "2"] as const;

/**
 * A quiz-walk question: tap 1, X or 2. The team may change its pick
 * until it continues; nothing says whether it is right until the end.
 */
export const QuizBlock: Renderer<"quiz"> = ({ block, ctx }) => {
  const { t } = ctx;
  const picked = block.picked;
  const final = !!block.final;
  const pick = (choice: string) => ctx.node && ctx.act?.({ kind: "pick", node: ctx.node, block: block.id, choice });
  return (
    <fieldset className={"pv-quiz" + (final ? " is-final" : "")}>
      <legend className="pv-quiz__q">
        <Markdown text={fillTeam(block.text, ctx)} />
      </legend>
      <div className="pv-quiz__options" role="radiogroup" aria-label={t("Answer")}>
        {CHOICES.map((c, i) => {
          const mine = picked === c;
          const right = block.answer === c;
          return (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={mine}
              className={"pv-quiz__option" + (mine ? " is-mine" : "") + (block.answer ? (right ? " is-right" : mine ? " is-wrong" : "") : "")}
              disabled={final || ctx.busy || !ctx.act}
              onClick={() => mine || pick(c)}
            >
              <span className="pv-quiz__mark">{c}</span>
              <span className="pv-quiz__text">{block.options[i]}</span>
            </button>
          );
        })}
      </div>
      <p className="pv-quiz__note">
        {final
          ? t("Answer locked in. You'll see how you did at the end.")
          : picked
            ? t("You can change your answer until you continue.")
            : t("Pick one. It locks when you continue.")}
      </p>
    </fieldset>
  );
};

/** A number question for breaking ties: one guess, which may change until the team continues. */
export const TiebreakBlock: Renderer<"tiebreak"> = ({ block, ctx }) => {
  const { t } = ctx;
  const [guess, setGuess] = useState(block.picked ?? "");
  const ok = /^-?\d+([.,]\d+)?$/.test(guess.trim()) && guess.trim() !== block.picked;
  return (
    <form
      className={"pv-quiz pv-tiebreak" + (block.final ? " is-final" : "")}
      onSubmit={(e) => {
        e.preventDefault();
        if (ok && ctx.node) ctx.act?.({ kind: "pick", node: ctx.node, block: block.id, choice: guess.trim() });
      }}
    >
      <span className="pv-quiz__tag">{t("Tie-breaker")}</span>
      <Markdown className="pv-quiz__q" text={fillTeam(block.text, ctx)} />
      {block.final ? (
        <p className="pv-quiz__note">
          {block.target !== undefined
            ? t("Your guess: {{guess}}. The answer: {{answer}}.", { guess: block.picked ?? "–", answer: block.target })
            : t("Your guess: {{guess}}. Locked in.", { guess: block.picked ?? "–" })}
        </p>
      ) : (
        <>
          <div className="pv-tiebreak__row">
            <input className="pv-input" inputMode="decimal" value={guess} placeholder={t("Your guess")} aria-label={t("Your guess")} onChange={(e) => setGuess(e.target.value)} />
            <button type="submit" className="pv-btn pv-btn--primary" disabled={!ok || ctx.busy || !ctx.act}>
              {block.picked ? t("Change") : t("Save")}
            </button>
          </div>
          <p className="pv-quiz__note">
            {block.picked ? t("Your guess: {{guess}}. You can change it until you continue.", { guess: block.picked }) : t("It locks when you continue.")}
          </p>
        </>
      )}
    </form>
  );
};

/** How the team did on the quiz walk: the score, then each question. */
export function QuizResult({ quiz, t }: { quiz: play.QuizResult; t: Translate }) {
  const mark = ["1", "X", "2"];
  const option = (item: play.QuizResult["items"][number], c: string | undefined) => (c ? `${c} · ${item.options[mark.indexOf(c)] ?? ""}` : t("no answer"));
  // A typed answer is kept only when right, so a wrong one shows the right answer alone.
  const verdict = (item: play.QuizResult["items"][number]) =>
    item.typed
      ? item.right
        ? item.picked
        : t("Right answer: {{answer}}", { answer: item.answer })
      : item.right
        ? option(item, item.answer)
        : t("You: {{picked}} — right: {{answer}}", { picked: option(item, item.picked), answer: option(item, item.answer) });
  return (
    <section className="pv-quiz pv-quiz-result">
      <h3 className="pv-quiz-result__head">{t("Quiz walk: {{right}} of {{total}} right", { right: quiz.right, total: quiz.total })}</h3>
      <ol>
        {quiz.items.map((item) => {
          const right = item.right;
          return (
            <li key={`${item.node}/${item.block}`} className={right ? "is-right" : "is-wrong"}>
              <span className="pv-quiz-result__verdict" aria-label={right ? t("Right") : t("Wrong")}>
                {right ? "✓" : "✗"}
              </span>
              <span className="pv-quiz-result__q">{item.question}</span>
              <span className="pv-quiz-result__a">{verdict(item)}</span>
            </li>
          );
        })}
      </ol>
      {quiz.tiebreak && (
        <p className="pv-quiz__note">
          {quiz.tiebreak.guess !== undefined
            ? t("Tie-breaker: you guessed {{guess}}, the answer was {{answer}}.", { guess: quiz.tiebreak.guess, answer: quiz.tiebreak.answer })
            : t("Tie-breaker: the answer was {{answer}}.", { answer: quiz.tiebreak.answer })}
        </p>
      )}
    </section>
  );
}
