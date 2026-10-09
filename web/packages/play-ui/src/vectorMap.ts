// Vector pictures for StaticMap: one hidden MapLibre map, shared by the
// whole page, draws each small map's area in turn and hands back a
// picture of it. A page can hold many maps but only a handful of WebGL
// contexts, so the maps never get one each.
//
// play-ui has no bundler of its own, so it does not load MapLibre
// itself: each app registers its loader with setVectorMapLoader. Without
// one, or without a vector style in window.ZV_MAP, StaticMap keeps to
// raster tiles.

/** The parts of MapLibre the pictures use. */
export interface VectorMapLib {
  Map: new (options: Record<string, unknown>) => SnapshotMap;
}

interface SnapshotMap {
  once(event: string, listener: () => void): unknown;
  jumpTo(options: { center: [number, number]; zoom: number }): unknown;
  resize(): unknown;
  triggerRepaint(): void;
  getCanvas(): HTMLCanvasElement;
  getStyle(): { sources?: Record<string, unknown> } | undefined;
  getSource(id: string): { attribution?: string } | undefined;
}

let loader: (() => Promise<VectorMapLib>) | null = null;

/** Lets StaticMap draw from the vector style, with the app's own MapLibre. */
export function setVectorMapLoader(load: () => Promise<VectorMapLib>): void {
  loader = load;
}

/** The operator's vector style, or none: then the maps stay raster. */
export function vectorStyle(): string | undefined {
  return (globalThis as { ZV_MAP?: { style_url?: string } }).ZV_MAP?.style_url || undefined;
}

/** Whether StaticMap should draw from the vector style. */
export const vectorMapsOn = (): boolean => loader !== null && !!vectorStyle() && typeof document !== "undefined";

let credit = "";
/** The vector style's credit as plain text, once a picture has been drawn. */
export const vectorCredit = (): string => credit;

let shared: Promise<{ map: SnapshotMap; box: HTMLDivElement }> | null = null;

function sharedMap(lib: VectorMapLib, style: string) {
  shared ??= new Promise((resolve, reject) => {
    const box = document.createElement("div");
    box.setAttribute("aria-hidden", "true");
    Object.assign(box.style, { position: "fixed", left: "-10000px", top: "0", width: "256px", height: "256px", pointerEvents: "none" });
    document.body.append(box);
    const map = new lib.Map({
      container: box,
      style,
      interactive: false,
      attributionControl: false,
      fadeDuration: 0,
      // Read back from the canvas after drawing, so it must keep it.
      canvasContextAttributes: { preserveDrawingBuffer: true },
      pixelRatio: Math.min(globalThis.devicePixelRatio || 1, 2),
    });
    map.once("error", () => {
      shared = null;
      box.remove();
      reject(new Error("vector map failed to load"));
    });
    map.once("load", () => resolve({ map, box }));
  });
  return shared;
}

/** The style's sources' credits as plain text. */
function creditOf(map: SnapshotMap): string {
  const ids = Object.keys(map.getStyle()?.sources ?? {});
  const parts = ids.map((id) => map.getSource(id)?.attribution ?? "").filter(Boolean);
  const html = [...new Set(parts)].join(" ");
  if (!html || typeof DOMParser === "undefined") return "";
  return (new DOMParser().parseFromString(html, "text/html").body.textContent ?? "").replace(/\s+/g, " ").trim();
}

const pictures = new Map<string, Promise<string | null>>();
let queue: Promise<unknown> = Promise.resolve();

const keyOf = (lng: number, lat: number, zoom: number, width: number, height: number) =>
  `${lng.toFixed(6)},${lat.toFixed(6)},${zoom},${Math.round(width)}x${Math.round(height)}`;

/**
 * A picture of the vector map centred on lng/lat at MapLibre's `zoom`,
 * width × height CSS pixels; null when it could not be drawn, so the
 * caller can fall back. Pictures are drawn one at a time and kept.
 */
export function vectorPicture(lng: number, lat: number, zoom: number, width: number, height: number): Promise<string | null> {
  const key = keyOf(lng, lat, zoom, width, height);
  const known = pictures.get(key);
  if (known) return known;
  const load = loader;
  const style = vectorStyle();
  if (!load || !style) return Promise.resolve(null);
  const p = queue
    .then(async () => {
      const lib = await load();
      const { map, box } = await sharedMap(lib, style);
      box.style.width = `${Math.round(width)}px`;
      box.style.height = `${Math.round(height)}px`;
      map.resize();
      map.jumpTo({ center: [lng, lat], zoom });
      await new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, 10000);
        map.once("idle", () => {
          clearTimeout(timer);
          resolve();
        });
        map.triggerRepaint();
      });
      // A source's credit arrives with its tile metadata, after the style:
      // read it once tiles have drawn.
      if (!credit) credit = creditOf(map);
      return map.getCanvas().toDataURL("image/png");
    })
    .catch(() => null);
  queue = p;
  pictures.set(key, p);
  // A failure is not kept: the next render tries again.
  void p.then((src) => {
    if (src === null) pictures.delete(key);
  });
  return p;
}
