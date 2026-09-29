import { useSyncExternalStore } from "react";

/**
 * A hash router, deliberately tiny.
 *
 * The routes are the classic console's, hash for hash: "#events/5" there
 * is "#/events/5" here, and parseRoute accepts both, so a bookmark or an
 * emailed link from either console lands on the same page. Hash URLs
 * survive the /app/arrange/ prefix without server-side rewrite rules.
 */
export type Route =
  | { name: "dashboard" }
  | { name: "adventures" }
  | { name: "adventure"; slug: string }
  | { name: "designer"; slug: string }
  | { name: "catalog" }
  | { name: "catalog-item"; slug: string }
  | { name: "events" }
  | { name: "event"; id: number }
  | { name: "event-control"; id: number }
  | { name: "quests" }
  | { name: "redemptions"; tab?: string }
  | { name: "organizations" }
  | { name: "organization"; id: number }
  | { name: "logs" }
  | { name: "settings"; tab?: string }
  | { name: "webhook-log"; endpoint: number }
  | { name: "not-found" };

function decode(part: string): string {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
}

const num = (s: string | undefined) => {
  const n = s ? Number.parseInt(s, 10) : NaN;
  return Number.isFinite(n) && n > 0 ? n : null;
};

export function parseRoute(hash: string): Route {
  const parts = hash.replace(/^#\/?/, "").split(/[/?]/).filter(Boolean).map(decode);
  const [section = "", arg, sub] = parts;
  switch (section) {
    case "":
    case "dashboard":
    // Stripe's checkout return; the dashboard's useJustPurchased reads it.
    case "purchase-success":
    case "purchase-cancelled":
      return { name: "dashboard" };
    case "adventures":
      if (!arg) return { name: "adventures" };
      // The designer is the adventure's own editor; the detail page is
      // its summary. "#/adventures/x/design" opens the designer.
      return sub === "design" ? { name: "designer", slug: arg } : { name: "adventure", slug: arg };
    case "design":
      return arg ? { name: "designer", slug: arg } : { name: "adventures" };
    case "catalog":
    case "store":
      return arg ? { name: "catalog-item", slug: arg } : { name: "catalog" };
    case "events": {
      const id = num(arg);
      if (!id) return { name: "events" };
      return sub === "control" ? { name: "event-control", id } : { name: "event", id };
    }
    case "quests": {
      const id = num(arg);
      return id ? { name: "event", id } : { name: "quests" };
    }
    case "redemptions":
      return arg ? { name: "redemptions", tab: arg } : { name: "redemptions" };
    case "organizations": {
      const id = num(arg);
      return id ? { name: "organization", id } : { name: "organizations" };
    }
    case "logs":
      return { name: "logs" };
    case "settings":
      return arg ? { name: "settings", tab: arg } : { name: "settings" };
    // Old top-level sections that became Settings tabs.
    case "members":
    case "admins":
    case "audit":
    case "purchases":
      return { name: "settings", tab: section };
    case "webhooks": {
      const id = arg === "log" ? num(sub) : null;
      return id ? { name: "webhook-log", endpoint: id } : { name: "settings", tab: "integrations" };
    }
    case "simulator":
    case "library":
      return arg ? { name: "adventure", slug: arg } : { name: "adventures" };
    default:
      return { name: "not-found" };
  }
}

export function href(r: Route): string {
  const e = encodeURIComponent;
  switch (r.name) {
    case "dashboard":
      return "#/";
    case "adventures":
      return "#/adventures";
    case "adventure":
      return `#/adventures/${e(r.slug)}`;
    case "designer":
      return `#/adventures/${e(r.slug)}/design`;
    case "catalog":
      return "#/catalog";
    case "catalog-item":
      return `#/catalog/${e(r.slug)}`;
    case "events":
      return "#/events";
    case "event":
      return `#/events/${r.id}`;
    case "event-control":
      return `#/events/${r.id}/control`;
    case "quests":
      return "#/quests";
    case "redemptions":
      return r.tab ? `#/redemptions/${e(r.tab)}` : "#/redemptions";
    case "organizations":
      return "#/organizations";
    case "organization":
      return `#/organizations/${r.id}`;
    case "logs":
      return "#/logs";
    case "settings":
      return r.tab ? `#/settings/${e(r.tab)}` : "#/settings";
    case "webhook-log":
      return `#/webhooks/log/${r.endpoint}`;
    case "not-found":
      return "#/";
  }
}

/** Go to a route, adding a history entry. */
export function navigate(r: Route) {
  location.hash = href(r);
}

/**
 * A page of the classic console, kept at /arrange/classic for its
 * list-based editor. Its hashes are this router's minus the leading slash.
 */
export function consoleHref(hash: string): string {
  return `/arrange/classic#${hash}`;
}

function subscribe(cb: () => void) {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash, () => "");
  return parseRoute(hash);
}
