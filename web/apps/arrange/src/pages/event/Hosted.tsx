import { useCallback, useEffect, useMemo, useState } from "react";
import { Game, HostedProvider, PersonaliseSection, PlacesSection, PlayerLink, WhereSection, adaptable, consoleApi, type HostedEvent } from "@zonventure/hosted";
import { useT } from "../../i18n.tsx";
import { Card, ErrorBox, Loading, useToast } from "../../ui/index.ts";

/**
 * A hosted event's host page, in the console: the same views its host
 * sees — the player link, the games, and what the host sets up — so an
 * arranger can set it up for a customer or help them run it. refresh
 * changes whenever the event does, to read it again.
 */
export function HostedCards({ eventId, refresh }: { eventId: number; refresh: number }) {
  const t = useT();
  const toast = useToast();
  const api = useMemo(() => consoleApi(eventId), [eventId]);
  const [hosted, setHosted] = useState<HostedEvent | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api.get().then(
      (h) => {
        setHosted(h);
        setError(null);
      },
      (e) => setError(e instanceof Error ? e.message : String(e)),
    );
  }, [api]);
  useEffect(load, [load, refresh]);

  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      try {
        await fn();
        load();
      } catch (e) {
        toast.err(e);
      } finally {
        setBusy(false);
      }
    },
    [load, toast],
  );

  if (error && !hosted) return <ErrorBox message={error} retry={load} retryLabel={t.t("Retry")} />;
  if (!hosted) return <Loading label={t.t("Loading…")} />;

  const personal = hosted.values.length > 0 || hosted.editables.length > 0;
  return (
    <HostedProvider value={{ i18n: t, api, run, busy }}>
      <div className="ev-cols">
        <div className="ev-col">
          <Card title={t.t("Games")}>
            <Game event={hosted} />
          </Card>
          <Card title={t.t("Player link")} subtitle={t.t("Teams join from it and name themselves.")}>
            <PlayerLink event={hosted} />
          </Card>
        </div>
        <div className="ev-col">
          {adaptable(hosted) && hosted.cards_ok && hosted.park_ok && (
            <Card title={t.t("Where it is played")}>
              <WhereSection party={hosted} />
            </Card>
          )}
          {hosted.mode === "park" && (
            <Card title={t.t("Places")}>
              <PlacesSection party={hosted} />
            </Card>
          )}
          {personal && (
            <Card title={t.t("The host's words")} subtitle={t.t("What the adventure lets the host make their own. It applies from the next game.")}>
              <PersonaliseSection party={hosted} />
            </Card>
          )}
        </div>
      </div>
    </HostedProvider>
  );
}
