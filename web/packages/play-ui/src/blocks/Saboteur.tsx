import type { CSSProperties } from "react";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * The saboteur's button: only their phone is sent it. It does its harm
 * once — points, a lockout, a false note — and says so only here.
 */
export const SabotageBlock: Renderer<"sabotage"> = ({ block, ctx }) => {
  const { t } = ctx;
  const done = !!block.picked;
  const press = () => ctx.node && ctx.act?.({ kind: "sabotage", node: ctx.node, block: block.id });
  return (
    <div className="pv-sabotage">
      <p className="pv-sabotage__tag">{t("Only you can see this")}</p>
      {block.text && <Markdown text={fillTeam(block.text, ctx)} />}
      <button type="button" className="pv-btn pv-sabotage__go" disabled={done || !ctx.live || ctx.busy || !ctx.act} onClick={press}>
        {done ? t("Done. No one saw.") : t("Do it")}
      </button>
      {!ctx.live && <p className="pv-vote__note">{t("The saboteur presses this on their own phone in a live event.")}</p>}
    </div>
  );
};

/**
 * Who is the saboteur? Each player names a teammate on their own phone —
 * never themselves — and may change their mind while the team is here.
 * What the team concluded counts when it finishes.
 */
export const AccuseBlock: Renderer<"accuse"> = ({ block, ctx }) => {
  const { t } = ctx;
  const suspects = block.suspects ?? [];
  const tally = block.tally ?? [];
  const cast = tally.reduce((s, n) => s + n, 0);
  const name = (slot: number) => ctx.node && ctx.act?.({ kind: "vote", node: ctx.node, block: block.id, choice: String(slot) });
  return (
    <fieldset className="pv-vote">
      <legend className="pv-vote__q">
        <Markdown text={fillTeam(block.text ?? "", ctx)} />
      </legend>
      <div className="pv-vote__options" role="radiogroup" aria-label={t("Who do you name?")}>
        {suspects.map((s, i) => {
          const mine = block.voted === String(s.slot);
          const n = tally[i] ?? 0;
          return (
            <button
              key={s.slot}
              type="button"
              role="radio"
              aria-checked={mine}
              className={"pv-vote__option" + (mine ? " is-mine" : "")}
              style={{ "--share": cast ? `${(n / cast) * 100}%` : "0%" } as CSSProperties}
              disabled={!ctx.live || ctx.busy || !ctx.act}
              onClick={() => mine || name(s.slot)}
            >
              <span className="pv-vote__bar" aria-hidden="true" />
              <span className="pv-vote__text">{s.name}</span>
              <span className="pv-vote__n">{n}</span>
            </button>
          );
        })}
      </div>
      <p className="pv-vote__note">
        {!ctx.live
          ? t("Players name a teammate from their own phones in a live event.")
          : t("{{n}} of {{m}} have named someone. It counts when your team finishes.", { n: cast, m: block.voters ?? 0 })}
      </p>
    </fieldset>
  );
};
