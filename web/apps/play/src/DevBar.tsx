import { useState } from "react";
import { play } from "@zonventure/api";
import type { Translate } from "@zonventure/play-ui";

/**
 * A dev ticket's bar: this phone plays the draft as it is edited, and
 * may go to any of its steps — back to one played, or on to one not
 * reached, played afresh — or start the adventure over. Never shown to
 * a team in a real event.
 */
export function DevBar({ dev, node, t, onMoved }: { dev: NonNullable<play.PlayState["dev"]>; node: string | undefined; t: Translate; onMoved: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const move = (go: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    go()
      .then(onMoved)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : t("Could not move.")))
      .finally(() => setBusy(false));
  };
  return (
    <div className="app-dev" role="region" aria-label={t("Draft preview")}>
      <span className="app-dev__tag" title={t("This phone plays the draft: changes show as they are saved.")}>DEV</span>
      <select
        className="app-dev__steps"
        aria-label={t("Go to step")}
        value={node ?? ""}
        disabled={busy}
        onChange={(e) => e.target.value && move(() => play.devGoTo(e.target.value))}
      >
        {!node && <option value="">{t("At the start")}</option>}
        {dev.steps.map((s, i) => (
          <option key={s.slug} value={s.slug}>
            {i + 1}. {s.title || (s.type === "end" ? t("The end") : s.slug)}
          </option>
        ))}
      </select>
      <button type="button" className="app-dev__reset" disabled={busy} onClick={() => move(() => play.devReset())}>
        {t("Start over")}
      </button>
      {error && <span className="app-dev__error">{error}</span>}
    </div>
  );
}
