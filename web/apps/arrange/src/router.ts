import { useSyncExternalStore } from "react";

/**
 * A hash router, deliberately tiny.
 *
 * The console has three screens today. A routing library would be more
 * code than the routes, and hash URLs survive the /app/arrange/ prefix
 * without any server-side rewrite rules — a deep link to a designer is
 * just /app/arrange/#/adventures/demo.
 */
export type Route =
  | { name: "dashboard" }
  | { name: "adventures" }
  | { name: "designer"; slug: string };

function decode(part: string): string {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
}

export function parseRoute(hash: string): Route {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decode);
  if (parts[0] === "adventures" && parts[1]) return { name: "designer", slug: parts[1] };
  if (parts[0] === "adventures") return { name: "adventures" };
  return { name: "dashboard" };
}

export function href(r: Route): string {
  switch (r.name) {
    case "dashboard":
      return "#/";
    case "adventures":
      return "#/adventures";
    case "designer":
      return `#/adventures/${encodeURIComponent(r.slug)}`;
  }
}

/**
 * A page of the classic console at /arrange. Events, settings and the
 * rest have not been ported here yet; until they are, this console links
 * to them there, and that one links back for the designer.
 */
export function consoleHref(hash: string): string {
  return `/arrange#${hash}`;
}

function subscribe(cb: () => void) {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash, () => "");
  return parseRoute(hash);
}
