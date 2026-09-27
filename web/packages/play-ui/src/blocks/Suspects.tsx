import { play } from "@zonventure/api";
import { struckCards, toggleCard } from "../gadgets.ts";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A board of suspects the team rules out one by one: tapping a card
 * strikes it, tapping again brings it back. What is struck is kept for
 * the whole team, on every phone; it never scores.
 */
export const SuspectsBlock: Renderer<"suspects"> = ({ block, ctx }) => {
  const { t } = ctx;
  const struck = new Set(struckCards(block.picked));
  const cards = block.cards ?? [];
  const strike = (i: number) => ctx.node && ctx.act?.({ kind: "pick", node: ctx.node, block: block.id, choice: toggleCard(block.picked, i) });
  const left = cards.length - struck.size;
  return (
    <section className="pv-suspects" aria-label={t("Suspects")}>
      <p className="pv-suspects__count" role="status">
        {left === 1 && cards.length > 1 ? t("One suspect left.") : t("{{n}} of {{m}} suspects left", { n: left, m: cards.length })}
      </p>
      <ul className="pv-suspects__grid">
        {cards.map((c, i) => {
          const out = struck.has(i);
          return (
            <li key={i}>
              <button
                type="button"
                className={"pv-suspects__card" + (out ? " is-struck" : "")}
                aria-pressed={out}
                disabled={ctx.busy || !ctx.act}
                onClick={() => strike(i)}
              >
                {c.media ? <img className="pv-suspects__img" src={play.mediaURL(c.media)} alt={c.alt ?? ""} loading="lazy" draggable={false} /> : <span className="pv-suspects__img pv-suspects__img--none" aria-hidden="true">?</span>}
                <span className="pv-suspects__name">{c.name}</span>
                {c.text && <Markdown className="pv-suspects__text" text={fillTeam(c.text, ctx)} />}
                <span className="pv-suspects__mark" aria-hidden="true">
                  {out ? t("Ruled out") : ""}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      <p className="pv-muted">{t("Tap a suspect to rule them out, and again to bring them back.")}</p>
    </section>
  );
};
