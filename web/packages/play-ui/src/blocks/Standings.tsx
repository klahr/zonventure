import type { Renderer } from "./context.ts";

const points = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

/** The team's place, and the teams just ahead and behind. Filled only during a real event. */
export const StandingsBlock: Renderer<"standings"> = ({ block, ctx }) => {
  const s = block.standings;
  const { t } = ctx;
  if (!s) return <p className="pv-standings pv-standings--none">{t("Standings show here during the event.")}</p>;
  return (
    <div className="pv-standings">
      <span className="pv-standings__rank">{t("{{rank}} of {{of}}", { rank: s.rank, of: s.of })}</span>
      {s.ahead && <span className="pv-standings__rival">{t("{{team}} is {{n}} points ahead", { team: s.ahead.name, n: points(s.ahead.gap) })}</span>}
      {s.behind && <span className="pv-standings__rival">{t("{{team}} is {{n}} points behind", { team: s.behind.name, n: points(s.behind.gap) })}</span>}
      {!s.ahead && s.of > 1 && <span className="pv-standings__rival">{t("You are in the lead!")}</span>}
    </div>
  );
};
