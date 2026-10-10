import { useSyncExternalStore } from "react";

/**
 * A hash router, deliberately tiny.
 *
 * The routes are the classic console's, hash for hash: "#events/5" there
 * is "#/events/5" here, and parseRoute accepts both, so a bookmark or an
 * emailed link from either console lands on the same page. Hash URLs
 * survive the /app/arrange/ prefix without server-side rewrite rules.
 */
/**
 * The shared catalog of adventures is switched off for now: no tab, no
 * links, and its old URLs land on Adventures (a commissioned adventure's
 * page still opens there). The server side is untouched, so flipping
 * this back brings it all back.
 */
export const CATALOG_ENABLED = false;

export type Route =
  | { name: "dashboard" }
  | { name: "adventures" }
  | { name: "adventure"; slug: string }
  | { name: "designer"; slug: string }
  | { name: "trail"; slug: string }
  | { name: "keep"; token: string }
  | { name: "field" }
  | { name: "authorize" }
  | { name: "catalog" }
  | { name: "catalog-item"; slug: string }
  | { name: "events" }
  | { name: "event"; id: number }
  | { name: "event-control"; id: number }
  | { name: "quests" }
  | { name: "parties"; slug?: string }
  | { name: "redemptions"; tab?: string }
  | { name: "organizations" }
  | { name: "organization"; id: number }
  | { name: "logs" }
  | { name: "listing-reviews"; tab?: string }
  | { name: "listing-review"; kind: "event" | "party" | "org" | "adventure"; id: number }
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
      // "#/adventures/x/cards" opens a quiz trail in the simple creator.
      if (sub === "cards") return { name: "trail", slug: arg };
      return sub === "design" ? { name: "designer", slug: arg } : { name: "adventure", slug: arg };
    case "design":
      return arg ? { name: "designer", slug: arg } : { name: "adventures" };
    case "field":
      return { name: "field" };
    // Keep it: a public quiz trail moving into this organisation.
    case "keep":
      return arg ? { name: "keep", token: arg } : { name: "adventures" };
    // An MCP client asking to sign in; its request is in the query.
    case "authorize":
      return { name: "authorize" };
    case "catalog":
    case "store":
      if (!CATALOG_ENABLED) return arg ? { name: "adventure", slug: arg } : { name: "adventures" };
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
    // "#/parties/<slug>" opens that adventure's offer over the list.
    case "parties":
      return arg ? { name: "parties", slug: arg } : { name: "parties" };
    case "redemptions":
      return arg ? { name: "redemptions", tab: arg } : { name: "redemptions" };
    case "organizations": {
      const id = num(arg);
      return id ? { name: "organization", id } : { name: "organizations" };
    }
    case "logs":
      return { name: "logs" };
    // Listing reviewers: listings waiting for review before they go
    // public; "#/listing-reviews/event/5" is one listing in full,
    // "#/listing-reviews/log" the recent decisions, "/reviewers" who reviews,
    // "/passkeys" the reviewer's own passkeys, which sign their decisions.
    case "listing-reviews": {
      const id = num(sub);
      if ((arg === "event" || arg === "party" || arg === "org" || arg === "adventure") && id) return { name: "listing-review", kind: arg, id };
      return arg === "log" || arg === "reviewers" || arg === "passkeys" ? { name: "listing-reviews", tab: arg } : { name: "listing-reviews" };
    }
    case "settings":
      // Parties was a Settings tab before it became its own section.
      if (arg === "parties") return { name: "parties" };
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
    case "trail":
      return `#/adventures/${e(r.slug)}/cards`;
    case "keep":
      return `#/keep/${e(r.token)}`;
    case "field":
      return "#/field";
    case "authorize":
      return "#/authorize";
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
    case "parties":
      return r.slug ? `#/parties/${e(r.slug)}` : "#/parties";
    case "redemptions":
      return r.tab ? `#/redemptions/${e(r.tab)}` : "#/redemptions";
    case "organizations":
      return "#/organizations";
    case "organization":
      return `#/organizations/${r.id}`;
    case "logs":
      return "#/logs";
    case "listing-reviews":
      return r.tab ? `#/listing-reviews/${e(r.tab)}` : "#/listing-reviews";
    case "listing-review":
      return `#/listing-reviews/${r.kind}/${r.id}`;
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

function subscribe(cb: () => void) {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash, () => "");
  return parseRoute(hash);
}
