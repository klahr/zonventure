import { useState } from "react";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A secret pick at a post. The players inside it pick one of two, once,
 * after a second tap to confirm; then every phone says the same, so a
 * screen shown to a teammate gives nothing away.
 */
export const MissionBlock: Renderer<"mission"> = ({ block, ctx }) => {
  const { t } = ctx;
  const [choice, setChoice] = useState<"0" | "1" | null>(null);
  const labels: [string, string] = [block.options?.[0] || t("Deliver"), block.options?.[1] || t("Tamper")];
  const need = block.need ?? Math.max(block.count ?? 1, 1);
  const seals = block.seals ?? 0;
  const full = seals >= need;
  const seal = (c: "0" | "1") => {
    setChoice(null);
    if (ctx.node) ctx.act?.({ kind: "seal", node: ctx.node, block: block.id, choice: c });
  };
  const playing = ctx.live || ctx.crew;
  const disabled = !playing || ctx.busy || !ctx.act;
  return (
    <div className="pv-vote pv-mission">
      {block.text && <Markdown text={fillTeam(block.text, ctx)} />}
      {block.sealed ? (
        <p className="pv-mission__done">{t("✓ Sealed. Head back to your team.")}</p>
      ) : full ? (
        <p className="pv-mission__done">{t("The mission is sealed.")}</p>
      ) : !playing ? (
        <p className="pv-vote__note">{t("Players pick at the post on their own phones in a live event.")}</p>
      ) : !block.here ? (
        <p className="pv-vote__note">{t("{{n}} of {{m}} have picked. Go to the post to pick.", { n: seals, m: need })}</p>
      ) : choice === null ? (
        <div className="pv-vote__options">
          {labels.map((label, i) => (
            <button key={i} type="button" className="pv-vote__option" disabled={disabled} onClick={() => setChoice(i === 0 ? "0" : "1")}>
              <span className="pv-vote__text">{label}</span>
            </button>
          ))}
        </div>
      ) : (
        <div className="pv-vote__options">
          <p className="pv-vote__note">{t("“{{choice}}”: you cannot change it.", { choice: labels[choice === "0" ? 0 : 1] })}</p>
          <button type="button" className="pv-btn pv-btn--primary" disabled={disabled} onClick={() => seal(choice)}>
            {t("Seal it")}
          </button>
          <button type="button" className="pv-btn" onClick={() => setChoice(null)}>
            {t("Back")}
          </button>
        </div>
      )}
    </div>
  );
};
