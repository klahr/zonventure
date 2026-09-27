import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A secret shared out between the team's phones: the server sends each
 * player only their parts. The simulator and the designer have no
 * player, and are sent every part, so they show them all, labelled.
 */
export const SplitBlock: Renderer<"split"> = ({ block, ctx }) => {
  const { t } = ctx;
  const parts = block.parts ?? [];
  if (!parts.length) return null;
  const player = block.player ?? 0;
  const players = block.players ?? 0;
  const mine = player > 0 && players > 0;
  return (
    <section className="pv-split" aria-label={t("A shared secret")}>
      <p className="pv-split__tag">
        {!mine
          ? t("Each player's phone shows a different part.")
          : players === 1
            ? t("You're playing alone, so you hold every part.")
            : t("You are player {{n}} of {{m}}. The others hold the rest: put it together.", { n: player, m: players })}
      </p>
      {parts.map((p, i) => (
        <div key={i} className="pv-split__part">
          {!mine && <span className="pv-split__who">{t("Player {{n}}", { n: i + 1 })}</span>}
          <Markdown className="pv-split__text" text={fillTeam(p, ctx)} />
        </div>
      ))}
    </section>
  );
};
