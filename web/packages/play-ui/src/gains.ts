import type { play } from "@zonventure/api";

/**
 * What the team has gained between two views: each item it now holds
 * more of, with qty the amount gained. Items used up or handed away are
 * not gains.
 */
export function gainedItems(before: readonly play.ItemView[] | undefined, after: readonly play.ItemView[] | undefined): play.ItemView[] {
  const had = new Map<string, number>();
  for (const it of before ?? []) had.set(it.slug, (had.get(it.slug) ?? 0) + it.qty);
  const out: play.ItemView[] = [];
  for (const it of after ?? []) {
    const more = it.qty - (had.get(it.slug) ?? 0);
    if (more > 0) out.push({ ...it, qty: more });
    had.set(it.slug, Math.max(0, (had.get(it.slug) ?? 0) - it.qty));
  }
  return out;
}

/** Gains shown together: a newer gain of the same item adds to it. */
export function mergeGains(shown: readonly play.ItemView[], more: readonly play.ItemView[]): play.ItemView[] {
  const out = shown.map((it) => ({ ...it }));
  for (const it of more) {
    const same = out.find((o) => o.slug === it.slug);
    if (same) same.qty += it.qty;
    else out.push({ ...it });
  }
  return out;
}
