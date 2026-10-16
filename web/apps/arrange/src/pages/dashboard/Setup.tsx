import type { ReactNode } from "react";
import { Check } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button } from "../../ui/index.ts";

/** A step's call to action: a link, or a button for a dialog. */
export type SetupAction = { label: string; href: string } | { label: string; onClick: () => void };

export interface SetupStepProps {
  done: boolean;
  title: string;
  hint?: string;
  action?: SetupAction | null;
}

/**
 * The "Getting started" checklist a first-time user sees on the
 * dashboard until the first event exists: each step one concrete
 * action with its why underneath, ticked off as it gets done.
 */
export function SetupCard({ lead, steps, foot }: { lead: string; steps: SetupStepProps[]; foot?: ReactNode }) {
  const t = useT();
  const done = steps.filter((s) => s.done).length;
  return (
    <section className="db-setup" aria-labelledby="db-setup-title">
      <div className="db-setup__head">
        <h2 id="db-setup-title" className="cs-card__title">{t.t("Getting started")}</h2>
        <span className="cs-sub">{t.t("{{done}} of {{total}} done", { done, total: steps.length })}</span>
      </div>
      <p className="cs-muted cs-flush">{lead}</p>
      <ol className="db-steps">
        {steps.map((s, i) => (
          <SetupStep key={i} n={i + 1} {...s} />
        ))}
      </ol>
      {foot}
    </section>
  );
}

function SetupStep({ n, done, title, hint, action }: SetupStepProps & { n: number }) {
  const t = useT();
  let cta: ReactNode = null;
  if (!done && action) {
    cta = "href" in action
      ? <a className="btn primary sm" href={action.href}>{action.label}</a>
      : <Button size="sm" variant="primary" onClick={action.onClick}>{action.label}</Button>;
  }
  return (
    <li className={"db-step" + (done ? " is-done" : "")}>
      <span className="db-step__dot" aria-hidden>{done ? <Check size={14} /> : n}</span>
      <span className="db-step__text">
        <span className="db-step__title">
          {title}
          {done && <span className="cs-sr"> — {t.t("done")}</span>}
        </span>
        {hint && <span className="cs-sub">{hint}</span>}
      </span>
      {cta && <span className="db-step__cta">{cta}</span>}
    </li>
  );
}
