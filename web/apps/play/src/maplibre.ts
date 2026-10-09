import { setVectorMapLoader, type VectorMapLib } from "@zonventure/play-ui";

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
      m.setWorkerUrl(new URL(worker.default, location.href).href);
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
