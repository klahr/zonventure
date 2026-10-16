import { useT } from "../../i18n.tsx";
import { Card, ErrorBox, Loading, Stat, Stats, Table, type Column } from "../../ui/index.ts";
import type { EventStats, PuzzleStats } from "./api.ts";
import { fmtElapsedShort, fmtPoints } from "./util.ts";

/**
 * How the game went, never naming a team: totals across everyone who
 * played, then each puzzle in the adventure's order with its solve rate,
 * wrong guesses and the commonest of them.
 */
export function StatisticsCard({ stats, error, retry }: { stats: EventStats | null; error?: string | null; retry: () => void }) {
  const t = useT();
  const loc = t.current();
  const pct = (n: number, of: number) => (of > 0 ? Math.round((n / of) * 100) + "%" : "—");
  const time = (s?: number) => (s === undefined ? "—" : fmtElapsedShort(s));
  const body = () => {
    if (error) return <ErrorBox message={error} retry={retry} retryLabel={t.t("Retry")} />;
    if (!stats) return <Loading label={t.t("Loading…")} />;
    if (stats.teams === 0) return <p className="cs-muted">{t.t("No team has played yet.")}</p>;
    const reached = stats.puzzles.filter((p) => p.reached > 0);
    // The trickiest takes the most wrong guesses per team that got there;
    // the quickest is solved fastest in the middle.
    const trickiest = reached.filter((p) => p.wrong > 0).sort((a, b) => b.wrong / b.reached - a.wrong / a.reached)[0];
    const quickest = reached.filter((p) => p.median_seconds !== undefined).sort((a, b) => a.median_seconds! - b.median_seconds!)[0];
    const columns: Column<PuzzleStats>[] = [
      { key: "title", header: t.t("Puzzle"), cell: (p) => p.title },
      { key: "reached", header: t.t("Reached"), align: "end", cell: (p) => p.reached },
      {
        key: "solved",
        header: t.t("Solved"),
        cell: (p) => (
          <span className="ev-statbar" title={`${p.solved}/${p.reached}`}>
            <span className="ev-statbar__track"><span className="ev-statbar__fill" style={{ width: p.reached > 0 ? `${(p.solved / p.reached) * 100}%` : 0 }} /></span>
            <span className="ev-statbar__n">{pct(p.solved, p.reached)}</span>
          </span>
        ),
      },
      { key: "first", header: t.t("First try"), align: "end", cell: (p) => pct(p.first_try, p.solved) },
      { key: "wrong", header: t.t("Wrong guesses"), align: "end", cell: (p) => p.wrong },
      { key: "hints", header: t.t("Hints"), align: "end", wide: true, cell: (p) => p.hints },
      { key: "time", header: t.t("Median time"), align: "end", cell: (p) => time(p.median_seconds) },
      {
        key: "answers",
        header: t.t("Commonest wrong guesses"),
        cell: (p) =>
          p.wrong_answers.length === 0 ? (
            <span className="cs-muted">—</span>
          ) : (
            <span className="ev-guesses">
              {p.wrong_answers.map((a) => (
                <span key={a.answer} className="ev-guess" title={a.teams === 1 ? t.t("{{n}}× by one team", { n: a.count }) : t.t("{{n}}× by {{teams}} teams", { n: a.count, teams: a.teams })}>
                  {a.answer}
                  {a.count > 1 && <span className="cs-muted"> ×{a.count}</span>}
                </span>
              ))}
            </span>
          ),
      },
    ];
    return (
      <>
        <Stats>
          <Stat value={stats.guesses} label={t.t("guesses")} />
          <Stat value={pct(stats.first_try, stats.solved)} label={t.t("solved on the first try")} />
          <Stat value={stats.hints} label={t.t("hints used")} />
          <Stat value={time(stats.median_seconds)} label={t.t("median finish time")} />
          {stats.avg_distance_m !== undefined && (
            <Stat value={(stats.avg_distance_m / 1000).toLocaleString(loc, { maximumFractionDigits: 1 }) + " km"} label={t.t("walked per team")} />
          )}
        </Stats>
        <ul className="ev-statfacts">
          <li>{t.t("{{n}} of {{of}} teams finished", { n: stats.finished, of: stats.teams })}{stats.failed > 0 && " · " + t.t("{{n}} failed", { n: stats.failed })}</li>
          {stats.fastest_seconds !== undefined && <li>{t.t("Fastest {{a}}, slowest {{b}}", { a: time(stats.fastest_seconds), b: time(stats.slowest_seconds) })}</li>}
          {stats.top_score !== undefined && <li>{t.t("Top score {{a}}, median {{b}}", { a: fmtPoints(stats.top_score), b: fmtPoints(stats.median_score) })}</li>}
          {trickiest && <li>{trickiest.wrong === 1 ? t.t("Trickiest: {{title}} — 1 wrong guess", { title: trickiest.title }) : t.t("Trickiest: {{title}} — {{n}} wrong guesses", { title: trickiest.title, n: trickiest.wrong })}</li>}
          {quickest && <li>{t.t("Quickest: {{title}} — solved in {{time}}", { title: quickest.title, time: time(quickest.median_seconds) })}</li>}
          {stats.photos > 0 && <li>{stats.photos === 1 ? t.t("1 photo taken") : t.t("{{n}} photos taken", { n: stats.photos })}</li>}
        </ul>
        <Table columns={columns} rows={stats.puzzles} rowKey={(p) => p.node} caption={t.t("Puzzles")} />
      </>
    );
  };
  return (
    <Card title={t.t("Statistics")} subtitle={t.t("Anonymous — across every team that played, without names.")}>
      {body()}
    </Card>
  );
}
