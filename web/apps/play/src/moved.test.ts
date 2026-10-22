import { test } from "node:test";
import assert from "node:assert/strict";
import { whoMoved, type Seen } from "./moved.ts";

const puzzle = (node: string, groups?: Seen["groups"], more: Partial<Seen> = {}): Seen => ({ node, type: "puzzle", ...(groups ? { groups } : {}), ...more });
const other = { moved_by_other: true };

test("a teammate's move is said, the team's or its group's", () => {
  assert.equal(whoMoved(puzzle("p1"), puzzle("p2", undefined, other)), "team");
  // Into a split, and set off from one by a groupmate.
  assert.equal(whoMoved(puzzle("p1"), { node: "split", type: "split", moved_by_other: true, groups: [{ port: "a", node: "split", current: true, moved_by_other: true }] }), "team");
  assert.equal(
    whoMoved({ node: "split", type: "split", groups: [{ port: "a", node: "split", current: true }] }, puzzle("pa", [{ port: "a", node: "pa", current: true, moved_by_other: true }])),
    "team",
  );
  // The others met at the join, the team taken on together.
  assert.equal(whoMoved({ node: "join", type: "join" }, puzzle("door", undefined, other)), "team");
  // A branch chosen at a hub.
  assert.equal(whoMoved({ node: "hub", type: "hub" }, puzzle("x1", undefined, other)), "team");
  // Through the join straight into another split, its paths named alike:
  // not the group the phone was shown.
  assert.equal(
    whoMoved(
      { node: "j1", type: "join", split: "s1", groups: [{ port: "a", node: "j1", current: true, arrived: true }] },
      { node: "s2", type: "split", split: "s2", moved_by_other: true, groups: [{ port: "a", node: "s2" }, { port: "b", node: "s2", current: true }] },
    ),
    "team",
  );
  // Through the join back into the same split for another round: its
  // groups dealt afresh, not the one the phone was shown.
  assert.equal(
    whoMoved(
      { node: "j", type: "join", split: "s", split_at: "10:00", groups: [{ port: "a", node: "j", current: true, arrived: true }] },
      { node: "s", type: "split", split: "s", split_at: "11:00", moved_by_other: true, groups: [{ port: "a", node: "s" }, { port: "b", node: "s", current: true }] },
    ),
    "team",
  );
  // Together again past the join.
  assert.equal(whoMoved(puzzle("pa", [{ port: "a", node: "pa", current: true }]), puzzle("door", undefined, other)), "team");
  // The phone's group moved on to wait at the join, the phone shown its other.
  assert.equal(
    whoMoved(
      puzzle("pb", [{ port: "a", node: "join", mine: true }, { port: "b", node: "pb", current: true, mine: true }]),
      puzzle("join", [{ port: "a", node: "join", current: true, mine: true }, { port: "b", node: "join", mine: true, moved_by_other: true }]),
    ),
    "team",
  );
  // The open group the phone walked, moved on to the join by another walking it.
  assert.equal(
    whoMoved(
      puzzle("pc", [{ port: "a", node: "pa", mine: true }, { port: "c", node: "pc", current: true }]),
      puzzle("pa", [{ port: "a", node: "pa", current: true, mine: true }, { port: "c", node: "join", arrived: true, moved_by_other: true }]),
    ),
    "team",
  );
});

test("the organisers' move is theirs", () => {
  assert.equal(whoMoved(puzzle("p1"), puzzle("p2", undefined, { moved_on: true })), "organisers");
  // The team moved into a split by them.
  assert.equal(whoMoved(puzzle("p1"), { node: "split", type: "split", moved_on: true, groups: [{ port: "a", node: "split", current: true, moved: true }] }), "organisers");
  // The phone's group moved on by them, the phone shown its other.
  assert.equal(
    whoMoved(
      puzzle("pb", [{ port: "a", node: "join", mine: true }, { port: "b", node: "pb", current: true, mine: true }]),
      puzzle("join", [{ port: "a", node: "join", current: true, mine: true }, { port: "b", node: "join", mine: true, moved: true }]),
    ),
    "organisers",
  );
  // An open group the phone walked, moved on by them.
  assert.equal(
    whoMoved(
      puzzle("pc", [{ port: "a", node: "pa", mine: true }, { port: "c", node: "pc", current: true }]),
      puzzle("pa", [{ port: "a", node: "pa", current: true, mine: true }, { port: "c", node: "join", arrived: true, moved: true }]),
    ),
    "organisers",
  );
});

test("the phone's own move, or no one's, is not said", () => {
  // Its own: no other phone moved it.
  assert.equal(whoMoved(puzzle("p1"), puzzle("p2")), null);
  assert.equal(whoMoved(puzzle("pa", [{ port: "a", node: "pa", current: true }]), puzzle("door2", [{ port: "a", node: "door2", current: true }])), null);
});

test("no one moving is not said", () => {
  // Shown another of the phone's groups, its own unmoved.
  assert.equal(
    whoMoved(puzzle("pc", [{ port: "a", node: "pa" }, { port: "c", node: "pc", current: true }]), puzzle("pa", [{ port: "a", node: "pa", current: true }, { port: "c", node: "pc" }])),
    null,
  );
  // Sent back to its own group from an open one, which its players came back to and moved on.
  assert.equal(
    whoMoved(
      puzzle("pc", [{ port: "a", node: "pa", mine: true }, { port: "c", node: "pc", current: true }]),
      puzzle("pa", [{ port: "a", node: "pa", current: true, mine: true }, { port: "c", node: "pc2", moved_by_other: true }]),
    ),
    null,
  );
  // A place reached is the team's arrival; the end, and the time limit, say so themselves.
  assert.equal(whoMoved({ node: "p1", type: "puzzle", zone: { slug: "z", lat: 0, lng: 0, radius_m: 50 }, here: false }, puzzle("p2", undefined, other)), null);
  assert.equal(whoMoved(puzzle("p1"), { node: "end", type: "end", finished: true, moved_by_other: true }), null);
  assert.equal(whoMoved(puzzle("p1"), { node: "accuse", type: "puzzle", timed_out: true }), null);
});
