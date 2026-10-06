/**
 * Embedded: the quiz trail creator running in a frame on the site's own
 * landing page. It then shows no header of its own, sizes to the frame,
 * and leaves the frame for what needs the whole window: paying (a
 * checkout page can't be framed), the admin page and My trails.
 */
export const embedded = (() => {
  try {
    return window.self !== window.top;
  } catch {
    return true;
  }
})();

/** Goes to url, out of the frame when embedded. */
export function leave(url: string) {
  try {
    (embedded && window.top ? window.top : window).location.href = url;
  } catch {
    window.location.href = url;
  }
}

/** The target for a link that leaves the frame. */
export const outside = embedded ? "_top" : undefined;

/**
 * How the landing page should size the frame, told by message (same
 * origin): "content", as tall as the page, so only the landing page
 * scrolls; "screen", the window's height, for the editor, whose bar and
 * card row stay pinned. Not embedded, it does nothing.
 */
export function fitFrame(root: HTMLElement, mode: "content" | "screen"): () => void {
  if (!embedded) return () => {};
  const post = () =>
    window.parent.postMessage({ type: "zv-trail-frame", mode, height: Math.ceil(root.getBoundingClientRect().height) }, location.origin);
  post();
  if (mode === "screen") return () => {};
  const ro = new ResizeObserver(post);
  ro.observe(root);
  return () => ro.disconnect();
}
