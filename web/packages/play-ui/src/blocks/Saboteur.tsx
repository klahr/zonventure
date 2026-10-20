import type { CSSProperties } from "react";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A sabotage: the same button on every phone, which anyone may press
 * once. Only the saboteur's press does harm — points, a lockout, a false
 * note — and every phone says the same after, so no screen tells.
 */
export const SabotageBlock: Renderer<"sabotage"> = ({ block, ctx }) => {
  const { t } = ctx;
  const done = !!block.sealed;
  const press = () => ctx.node && ctx.act?.({ kind: "sabotage", node: ctx.node, block: block.id });
  return (
    <div className="pv-sabotage">
      {block.text && <Markdown text={fillTeam(block.text, ctx)} />}
      <button type="button" className="pv-btn pv-sabotage__go" disabled={done || !(ctx.live || ctx.crew) || ctx.busy || !ctx.act} onClick={press}>
        {done ? t("Done. No one saw.") : t("Do it")}
      </button>
      {!ctx.live && !ctx.crew && <p className="pv-vote__note">{t("Every player sees this on their own phone in a live event; only the saboteur's press does anything.")}</p>}
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
