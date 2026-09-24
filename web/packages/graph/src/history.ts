// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Adapted from dazyflow web/src/lib/graphHistory.ts. The algorithm —
// whole-document snapshots, content-derived coalescing, rebase on
// external change — is unchanged. What changed is the delta classifier:
// dazyflow's nodes carry an untyped `params` bag and frames; ours carry
// typed per-node configs and x/y coordinates.

import { sameData as sameJSON } from "./reconcile.ts";
import type { Graph } from "./types.ts";

// Whole-document snapshots rather than a command stack: the editor mutates
// state from many places, and a command per mutation would drift from what
// actually changed. A snapshot is always accurate.

export type HistoryDoc = Graph;

/**
 * How one document differs from the next. Drives coalescing: a continuous
 * gesture (dragging, typing) must collapse into a single undo step, or one
 * Ctrl+Z would rewind a single mouse-move or a single keystroke.
 */
export type DeltaKind =
  | { kind: "none" }
  /** Only node positions moved — a drag. */
  | { kind: "positions" }
  /** Exactly one node's content changed — typing in the inspector. */
  | { kind: "node"; slug: string }
  /** Anything else: add, delete, connect, disconnect, resources. */
  | { kind: "structure" };

interface HistoryEntry {
  doc: HistoryDoc;
  delta: DeltaKind;
  at: number;
}

export interface HistoryState {
  past: HistoryEntry[];
  present: HistoryDoc | null;
  future: HistoryEntry[];
  presentDelta: DeltaKind;
  presentAt: number;
}

/**
 * How long a run of same-kind edits keeps merging into one undo step. Long
 * enough to swallow a slow drag or unhurried typing; short enough that a
 * deliberate pause starts a fresh step.
 */
export const COALESCE_WINDOW_MS = 600;

export const HISTORY_LIMIT = 200;

export function emptyHistory(): HistoryState {
  return { past: [], present: null, future: [], presentDelta: { kind: "none" }, presentAt: 0 };
}

function withoutPositions(g: HistoryDoc): unknown {
  return { ...g, nodes: g.nodes.map((n) => ({ ...n, x: undefined, y: undefined })) };
}

/**
 * classifyDelta reports what changed between two documents.
 *
 * "structure" is the fallback so an unrecognised change is never
 * coalesced away. Being wrong in that direction costs an extra undo step;
 * being wrong the other way would silently merge a delete into a drag.
 */
export function classifyDelta(prev: HistoryDoc | null, next: HistoryDoc): DeltaKind {
  if (!prev) return { kind: "structure" };
  if (sameJSON(prev, next)) return { kind: "none" };

  const sameNodeList =
    prev.nodes.length === next.nodes.length &&
    prev.nodes.every((n, i) => n.slug === next.nodes[i]!.slug);

  if (sameNodeList && sameJSON(withoutPositions(prev), withoutPositions(next))) {
    return { kind: "positions" };
  }

  // One node's content changed and nothing else did: an inspector edit.
  if (sameNodeList && sameJSON(prev.edges, next.edges)) {
    const changed = prev.nodes
      .map((n, i) => ({ slug: n.slug, same: sameJSON(n, next.nodes[i]) }))
      .filter((c) => !c.same);
    const restSame =
      sameJSON(prev.zones, next.zones) &&
      sameJSON(prev.items, next.items) &&
      sameJSON(prev.counters, next.counters) &&
      sameJSON(prev.journal, next.journal);
    if (changed.length === 1 && restSame) {
      return { kind: "node", slug: changed[0]!.slug };
    }
  }

  return { kind: "structure" };
}

function coalescible(a: DeltaKind, b: DeltaKind): boolean {
  if (a.kind !== b.kind) return false;
  if (a.kind === "positions") return true;
  if (a.kind === "node" && b.kind === "node") return a.slug === b.slug;
  return false;
}

/**
 * Fold a new document into the history. Returns the same state object when
 * nothing changed, so a caller can skip a re-render by reference equality —
 * selection-only changes reach here constantly and must not create steps.
 */
export function record(state: HistoryState, doc: HistoryDoc, now: number): HistoryState {
  if (state.present === null) return rebase(doc, now);
  const delta = classifyDelta(state.present, doc);
  if (delta.kind === "none") return state;

  if (coalescible(state.presentDelta, delta) && now - state.presentAt <= COALESCE_WINDOW_MS) {
    return { ...state, present: doc, presentDelta: delta, presentAt: now, future: [] };
  }

  const past = [...state.past, { doc: state.present, delta: state.presentDelta, at: state.presentAt }];
  while (past.length > HISTORY_LIMIT) past.shift();
  return { past, present: doc, future: [], presentDelta: delta, presentAt: now };
}

export const canUndo = (s: HistoryState) => s.past.length > 0;
export const canRedo = (s: HistoryState) => s.future.length > 0;

export function undo(state: HistoryState): { state: HistoryState; doc: HistoryDoc } | null {
  if (!state.past.length || state.present === null) return null;
  const past = state.past.slice();
  const prev = past.pop()!;
  return {
    state: {
      past,
      present: prev.doc,
      future: [{ doc: state.present, delta: state.presentDelta, at: state.presentAt }, ...state.future],
      presentDelta: prev.delta,
      presentAt: prev.at,
    },
    doc: prev.doc,
  };
}

export function redo(state: HistoryState): { state: HistoryState; doc: HistoryDoc } | null {
  if (!state.future.length || state.present === null) return null;
  const future = state.future.slice();
  const next = future.shift()!;
  return {
    state: {
      past: [...state.past, { doc: state.present, delta: state.presentDelta, at: state.presentAt }],
      present: next.doc,
      future,
      presentDelta: next.delta,
      presentAt: next.at,
    },
    doc: next.doc,
  };
}

/**
 * Discard the stack and adopt doc as the baseline — after a load, a
 * reload, or any change that arrived from outside the editor. Undoing past
 * someone else's edit would silently clobber it, so the stack is fenced.
 */
export function rebase(doc: HistoryDoc, now: number): HistoryState {
  return { past: [], present: doc, future: [], presentDelta: { kind: "none" }, presentAt: now };
}
