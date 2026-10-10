import { useState } from "react";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Pill } from "../../ui/index.ts";
import { verifyResults, type ResultsReport } from "./api.ts";

/**
 * The event's tamper-evident results record (internal/results): every
 * answer, hint, score change and finish, hash-chained as it happened.
 * Checking it recomputes the chain and compares each team's result now
 * with the one recorded; a finished team's receipt leads to the event's
 * fingerprint.
 */
export function ResultsRecordCard({ eventId }: { eventId: number }) {
  const t = useT();
  const [report, setReport] = useState<ResultsReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const check = async () => {
    setBusy(true);
    setError("");
    try {
      setReport(await verifyResults(eventId));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card
      title={t.t("Results record")}
      subtitle={t.t("Every answer, hint, score change and finish is recorded as it happens, each entry sealed with the one before it. Teams get a receipt when they finish.")}
      actions={<Button onClick={check} disabled={busy}>{busy ? t.t("Checking…") : t.t("Check results")}</Button>}
    >
      {error && <ErrorBox message={error} />}
      {report && (
        <div role="status">
          {report.ok ? (
            <p><Pill tone="good">{t.t("Intact")}</Pill> {t.t("All {{n}} entries add up, and every team's result is the one recorded.", { n: report.entries })}</p>
          ) : (
            <>
              <p><Pill tone="bad">{t.t("Changed")}</Pill> {t.t("The results were changed outside the game:")}</p>
              <ul>
                {report.problems.map((p, i) => (
                  <li key={i}>
                    {p.seq ? `#${p.seq} ` : ""}
                    {p.team_id ? (report.teams.find((x) => x.team_id === p.team_id)?.name ?? `#${p.team_id}`) + ": " : ""}
                    {p.problem}
                  </li>
                ))}
              </ul>
            </>
          )}
          <p>{report.sealed ? t.t("The event's results fingerprint:") : t.t("The record so far (the event hasn't ended):")}</p>
          <p><code className="ev-hash">{report.fingerprint || "—"}</code></p>
        </div>
      )}
    </Card>
  );
}
