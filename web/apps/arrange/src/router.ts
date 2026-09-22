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

export function parseRoute(hash: string): Route {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
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

export function navigate(r: Route): void {
  window.location.hash = href(r);
}

function subscribe(cb: () => void) {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash, () => "");
  return parseRoute(hash);
}
