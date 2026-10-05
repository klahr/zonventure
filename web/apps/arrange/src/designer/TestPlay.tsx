import { useEffect, useState } from "react";
import { adventures, type play } from "@zonventure/api";
import type { Envelope } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { ErrorBox, Loading, errorText } from "../ui/index.ts";
import { FloatingWindow } from "./FloatingWindow.tsx";
import { Simulator } from "./Simulator.tsx";
import "./designer.css";

type Ready = { envelope: Envelope; brand: play.Brand; title: string } | "empty";

/**
 * The designer's test play, opened from the adventure's page: the saved
 * draft played from Start in the same floating simulator. Lazy-loaded,
 * so the simulator and its styles come only when asked for.
 */
export default function TestPlay({ slug, title, onClose }: { slug: string; title: string; onClose: () => void }) {
  const t = useT();
  const [ready, setReady] = useState<Ready | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    Promise.all([adventures.getGraph<Envelope, unknown>(slug, ac.signal), adventures.getLook(slug, ac.signal).catch(() => null)])
      .then(([g, look]) => {
        if (ac.signal.aborted) return;
        setReady(!g ? "empty" : { envelope: g.envelope, brand: look?.brand ?? {}, title: look?.title || title });
      })
      .catch((e: unknown) => {
        if (!ac.signal.aborted) setError(errorText(e));
      });
    return () => ac.abort();
  }, [slug, title]);

  return (
    <FloatingWindow title={t.t("Test play")} storageKey="zonventure.preview.pos" onClose={onClose}>
      {error || ready === "empty" ? (
        <ErrorBox message={error ?? t.t("Nothing to play yet. Build the adventure in the designer first.")} />
      ) : !ready ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <Simulator
          slug={slug}
          graph={ready.envelope.graph}
          envelope={ready.envelope}
          selected={null}
          mode="run"
          restart={0}
          onPosition={() => {}}
          brand={ready.brand}
          title={ready.title}
        />
      )}
    </FloatingWindow>
  );
}
