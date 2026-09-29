import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, NetworkError } from "@zonventure/api";

export interface Load<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  /** Fetch again, keeping the current data on screen meanwhile. */
  reload: () => void;
  /** Replace the data locally, after a mutation that returned it. */
  set: (d: T) => void;
}

/**
 * Fetch for a page or a card. Aborts on unmount and when deps change, so
 * a slow answer for the previous id never lands on the next page.
 * A reload keeps the old data on screen — no flash of "Loading…" after
 * every save.
 */
export function useLoad<T>(fetcher: (signal: AbortSignal) => Promise<T>, deps: unknown[]): Load<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const ref = useRef(fetcher);
  ref.current = fetcher;

  useEffect(() => {
    const ac = new AbortController();
    setLoading(true);
    ref.current(ac.signal).then(
      (d) => {
        if (ac.signal.aborted) return;
        setData(d);
        setError(null);
        setLoading(false);
      },
      (e: unknown) => {
        if (ac.signal.aborted) return;
        setError(errorText(e));
        setLoading(false);
      },
    );
    return () => ac.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { data, error, loading, reload, set: setData };
}

/** A message for a failed request, fit to show the user. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof NetworkError) return "Could not reach the server. Check your connection.";
  return e instanceof Error ? e.message : String(e);
}
