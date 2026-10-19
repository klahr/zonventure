// The rules of an order puzzle, apart from the screen: a stack is a list
// where stack[place] is the card shown there, by its dealt position. The
// phone does not know the right order — the server checks the stack it
// is sent — so these only move cards about.

export type Stack = number[];

/** The cards as dealt: n of them, top to bottom. */
export const dealt = (n: number): Stack => Array.from({ length: n }, (_, i) => i);

/** Take the card at from out of the stack and put it in at to. */
export function moveCard(stack: Stack, from: number, to: number): Stack {
  const next = stack.slice();
  const [card] = next.splice(from, 1);
  next.splice(to, 0, card!);
  return next;
}

/**
 * Where a card dragged from `from` lands: the place among the others
 * whose middles are above its own middle, at `centre`. mids are every
 * card's middle as the drag began, top to bottom.
 */
export function dropPlace(mids: number[], from: number, centre: number): number {
  let to = 0;
  mids.forEach((m, i) => {
    if (i !== from && m < centre) to++;
  });
  return to;
}

/**
 * How far, in cards, the card at i makes way while the card from `from`
 * hovers over place `to`: up one, down one, or not at all.
 */
export function makesWay(i: number, from: number, to: number): -1 | 0 | 1 {
  if (from < to && i > from && i <= to) return -1;
  if (to < from && i >= to && i < from) return 1;
  return 0;
}

/** A saved stack, if it is still an order of n cards. */
export function restore(saved: unknown, n: number): Stack | null {
  if (!Array.isArray(saved) || saved.length !== n) return null;
  const seen = new Set<number>();
  for (const v of saved) {
    if (!Number.isInteger(v) || v < 0 || v >= n || seen.has(v)) return null;
    seen.add(v);
  }
  return saved as Stack;
}

/** The guess sent to the server: the dealt positions, top to bottom. */
export const guessFor = (stack: Stack) => stack.join(",");
