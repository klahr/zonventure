// The rules of a tile puzzle, apart from the screen: an order is a list
// where order[slot] is the tile shown in that slot, and tile i belongs in
// slot i. The server checks the order it is sent; these only decide what
// the phone shows.

export type Order = number[];

export const isSolved = (order: Order) => order.every((tile, slot) => tile === slot);

/** Swap the tiles in two slots. */
export function swap(order: Order, a: number, b: number): Order {
  const next = order.slice();
  [next[a], next[b]] = [next[b]!, next[a]!];
  return next;
}

/** A random order that is not already solved. */
export function shuffled(n: number, random: () => number = Math.random): Order {
  const order = Array.from({ length: n }, (_, i) => i);
  if (n < 2) return order;
  do {
    for (let i = n - 1; i > 0; i--) {
      const j = Math.floor(random() * (i + 1));
      [order[i], order[j]] = [order[j]!, order[i]!];
    }
  } while (isSolved(order));
  return order;
}

/**
 * A saved order, if it is still a puzzle of this size and not already
 * solved. A solved one is dropped: showing a finished picture with
 * nothing to do helps no one, and re-sending it is the server's call.
 */
export function restore(saved: unknown, n: number): Order | null {
  if (!Array.isArray(saved) || saved.length !== n) return null;
  const seen = new Set<number>();
  for (const v of saved) {
    if (!Number.isInteger(v) || v < 0 || v >= n || seen.has(v)) return null;
    seen.add(v);
  }
  return isSolved(saved as Order) ? null : (saved as Order);
}

/** The guess sent to the server: the order, comma-separated. */
export const guessFor = (order: Order) => order.join(",");
