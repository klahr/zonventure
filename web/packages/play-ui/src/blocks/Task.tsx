import { useState, type FormEvent } from "react";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * One role's part of a step done in parallel. Its player answers it here,
 * at its place where it has one; every other phone sees whose it is and
 * when it is done.
 */
export const TaskBlock: Renderer<"task"> = ({ block, ctx }) => {
  const { t } = ctx;
  const [guess, setGuess] = useState("");
  const role = ctx.roles?.find((r) => r.slug === block.role)?.name || block.role;
  const done = !!block.picked;
  const away = !!block.zone && block.here === false;
  const send = (e: FormEvent) => {
    e.preventDefault();
    if (!guess.trim() || !ctx.node) return;
    ctx.act?.({ kind: "task", node: ctx.node, block: block.id, guess: guess.trim() });
    setGuess("");
  };
  return (
    <div className={"pv-vote pv-task" + (done ? " is-done" : "")}>
      <p className="pv-task__who">{done ? t("✓ {{role}}'s task: done", { role }) : t("{{role}}'s task", { role })}</p>
      {block.text && (block.mine || done) && <Markdown text={fillTeam(block.text, ctx)} />}
      {!done &&
        (!block.mine ? (
          <p className="pv-vote__note">{t("The {{role}} is working on it.", { role })}</p>
        ) : away ? (
          <p className="pv-vote__note">{t("Go to {{place}} to do it.", { place: block.place?.name || t("its place") })}</p>
        ) : (
          <form className="pv-task__form" onSubmit={send}>
            <input className="pv-input" value={guess} onChange={(e) => setGuess(e.target.value)} placeholder={t("Your answer")} aria-label={t("Your answer")} disabled={ctx.busy || !ctx.act} />
            <button type="submit" className="pv-btn pv-btn--primary" disabled={ctx.busy || !ctx.act || !guess.trim()}>
              {t("Answer")}
            </button>
          </form>
        ))}
    </div>
  );
};
