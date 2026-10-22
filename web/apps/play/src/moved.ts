import type { play } from "@zonventure/api";

/** The parts of a view whoMoved reads (play.View): the server's names, checked against its type. */
export type Seen = Partial<Pick<play.View, "node" | "type" | "zone" | "here" | "finished" | "timed_out" | "split" | "split_at" | "moved_on" | "moved_by_other">> & {
  groups?: Pick<play.GroupView, "port" | "node" | "current" | "mine" | "arrived" | "moved" | "moved_by_other">[];
};

/**
 * whoMoved says who moved the team on from a step the player could act
 * on, or wait at — a puzzle, a story, a hub, a split, a join — when a
 * fresh view no longer shows it,
 * so the phone says so rather than swapping the step under them: a
 * teammate ("team") or the organisers ("organisers"), as the server marks
 * who moved the team or a group onto each step; null for the phone itself,
 * for no one — a timer — for what says so itself — the end, the time
 * limit's panel — or for leaving a place the team was walking to, its
 * arrival.
 *
 * Moved on is the group the phone was shown: its step now another — the
 * phone shown it still, or with it, or it come to its join — or the team
 * split up or together again; not a phone shown another of its groups,
 * which saw no one move, nor one sent back to its own from an open group
 * its players came back to and moved on.
 */
export function whoMoved(prev: Seen | null | undefined, next: Seen | null | undefined): "team" | "organisers" | null {
  if (!prev?.node || !next?.node || prev.node === next.node || next.finished || (next.timed_out && !prev.timed_out)) return null;
  const working = ["puzzle", "story", "hub", "split", "join"].includes(prev.type ?? "") && !(prev.zone && !prev.here);
  if (!working) return null;
  const was = prev.groups?.find((g) => g.current)?.port;
  // The same group: of the same split, and the same time at it, its path
  // named alike.
  const g = was && next.split === prev.split && next.split_at === prev.split_at ? next.groups?.find((g) => g.port === was) : undefined;
  // The team moved — split up, or together again — or the group shown.
  const by = !was || !g ? { moved: next.moved_on, moved_by_other: next.moved_by_other } : g;
  if (g && (g.node === prev.node || !(g.current || g.mine || g.arrived))) return null;
  if (by.moved) return "organisers";
  return by.moved_by_other ? "team" : null;
}
