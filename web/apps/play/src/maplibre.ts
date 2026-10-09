import { setVectorMapLoader, type VectorMapLib } from "@zonventure/play-ui";

/**
 * The worker's address, naming the map host. The worker downloads the
 * tiles under the Content-Security-Policy it was served with, and the
 * file is cached as immutable: without the host in its address, a
 * browser keeps a copy whose policy allows only the old map host.
 */
function workerURL(file: string): string {
  const url = new URL(file, location.href);
  const c = (globalThis as { ZV_MAP?: { style_url?: string; tile_url?: string } }).ZV_MAP;
  try {
    url.searchParams.set("map", new URL(c?.style_url || c?.tile_url || "").host);
  } catch {
    /* no map source: the plain address */
  }
  return url.href;
}

/**
 * MapLibre for the small maps' vector pictures (play-ui's vectorMap.ts),
 * loaded the first time one is drawn so a step without a map never pays
 * for it. MapLibre looks for its worker beside its own module, which
 * bundling moves; point it at the worker Vite builds instead.
 */
let loading: Promise<VectorMapLib> | null = null;
function loadMapLibre(): Promise<VectorMapLib> {
  loading ??= Promise.all([import("maplibre-gl"), import("maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url")]).then(
    ([m, worker]) => {
      m.setWorkerUrl(workerURL(worker.default));
      return m as unknown as VectorMapLib;
    },
    (e: unknown) => {
      // Forget the failure, so the next map tries again.
      loading = null;
      throw e;
    },
  );
  return loading;
}

setVectorMapLoader(loadMapLibre);
