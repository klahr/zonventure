import type { CSSProperties } from "react";
import { majority } from "../gadgets.ts";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A team vote: each player votes on their own phone, may change their
 * mind, and everyone sees the tally. Once all have voted the winner is
 * marked. It never routes. Votes need the players' own phones, so the
 * simulator and the designer only show it (ctx.live).
 */
export const VoteBlock: Renderer<"vote"> = ({ block, ctx }) => {
  const { t } = ctx;
  const tally = block.tally ?? [];
  const cast = tally.reduce((s, n) => s + n, 0);
  const voters = block.voters ?? 0;
  const everyone = voters > 0 && cast >= voters;
  const winner = everyone ? majority(tally) : -1;
  // The server leaves out an empty question or option list.
  const options = block.options ?? [];
  const vote = (i: number) => ctx.node && ctx.act?.({ kind: "vote", node: ctx.node, block: block.id, choice: String(i) });
  return (
    <fieldset className="pv-vote">
      <legend className="pv-vote__q">
        <Markdown text={fillTeam(block.text ?? "", ctx)} />
      </legend>
      <div className="pv-vote__options" role="radiogroup" aria-label={t("Your vote")}>
        {options.map((o, i) => {
          const mine = block.voted === String(i);
          const n = tally[i] ?? 0;
          return (
            <button
              key={i}
              type="button"
              role="radio"
              aria-checked={mine}
              className={"pv-vote__option" + (mine ? " is-mine" : "") + (winner === i ? " is-winner" : "")}
              style={{ "--share": cast ? `${(n / cast) * 100}%` : "0%" } as CSSProperties}
              disabled={!ctx.live || ctx.busy || !ctx.act}
              onClick={() => mine || vote(i)}
            >
              <span className="pv-vote__bar" aria-hidden="true" />
              <span className="pv-vote__text">{o}</span>
              <span className="pv-vote__n">{n}</span>
            </button>
          );
        })}
      </div>
      <p className="pv-vote__note">
        {!ctx.live
          ? t("Players vote from their own phones in a live event.")
          : everyone
            ? winner >= 0
              ? t("Everyone has voted: {{option}} it is.", { option: options[winner] ?? "" })
              : t("Everyone has voted, and it's a tie.")
            : t("{{n}} of {{m}} have voted.", { n: cast, m: voters })}
      </p>
    </fieldset>
  );
};
