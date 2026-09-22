import { useEffect, useState } from "react";

interface VersionResponse {
  version: string;
  commit?: string;
  modified?: boolean;
}

/**
 * Phase 0 seam check.
 *
 * This is deliberately thin. Its job is to prove the whole pipeline
 * end to end — Vite builds, `go:embed` picks the output up, the Go
 * router serves it under /app/arrange/, and the bundle can reach the
 * existing JSON API with the session cookie — before any real console
 * work starts in Phase 1.
 *
 * /api/version is the right probe: it is unauthenticated, it is the
 * endpoint monitoring already pins to, and rendering the running
 * binary's version proves the React bundle and the Go binary that
 * embedded it are the same build.
 */
export function App() {
  const [version, setVersion] = useState<VersionResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    fetch("/api/version", { signal: ac.signal, credentials: "same-origin" })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<VersionResponse>;
      })
      .then(setVersion)
      .catch((e: unknown) => {
        if (ac.signal.aborted) return;
        setError(e instanceof Error ? e.message : String(e));
      });
    return () => ac.abort();
  }, []);

  return (
    <main className="shell">
      <h1>Zonventure</h1>
      <p className="lede">React shell — Phase 0 seam.</p>
      <dl>
        <dt>Server</dt>
        <dd>
          {error !== null
            ? `unreachable (${error})`
            : version
              ? `${version.version}${version.modified ? " (modified)" : ""}`
              : "…"}
        </dd>
      </dl>
    </main>
  );
}
