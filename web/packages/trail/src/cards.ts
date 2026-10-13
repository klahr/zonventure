/**
 * A quiz trail's cards, as internal/trail has them: a question with three
 * options for 1, X and 2; a question with a typed answer; or a card to
 * read. The rules here mirror the server's (trail.Check, trail.Problems)
 * so the creator can say at once what a card still needs.
 */

export type Kind = "quiz" | "answer" | "info";

export interface Card {
  id: string;
  kind: Kind;
  text: string;
  options?: string[];
  correct?: string;
  answers?: string[];
  /** A template's card not yet changed; CardEditor drops it on the first edit. */
  example?: boolean;
}

export const CHOICES = ["1", "X", "2"] as const;

/**
 * How a mark reads: 1, X and 2 are a Swedish quiz walk's, so the Swedish
 * catalog turns the keys A, B and C back into them; other languages read
 * A, B, C. What is stored stays 1, X or 2.
 */
export const markLabel = (t: (key: string) => string, mark: string) => {
  const i = (CHOICES as readonly string[]).indexOf(mark);
  return i < 0 ? mark : t(["A", "B", "C"][i]!);
};

export const MAX_CARDS = 40;
export const MAX_TEXT = 300;
export const MAX_OPTION = 80;
export const MAX_ANSWERS = 5;

/** What keeps a card, or the trail (no card), from being played. */
export interface Problem {
  card?: string;
  code: "text" | "options" | "correct" | "answers" | "empty" | "no_question";
}

export function newId(): string {
  const b = new Uint8Array(4);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** A fresh card of kind. */
export function blank(kind: Kind): Card {
  if (kind === "quiz") return { id: newId(), kind, text: "", options: ["", "", ""], correct: "" };
  if (kind === "answer") return { id: newId(), kind, text: "", answers: [""] };
  return { id: newId(), kind, text: "" };
}

/** card as another kind, keeping its text and what fits. */
export function asKind(card: Card, kind: Kind): Card {
  if (card.kind === kind) return card;
  const out = { ...blank(kind), id: card.id, text: card.text };
  if (kind === "quiz" && card.options?.length === 3) return { ...out, options: card.options, correct: card.correct ?? "" };
  if (kind === "answer" && card.answers?.length) return { ...out, answers: card.answers };
  return out;
}

export function problems(cards: Card[]): Problem[] {
  const out: Problem[] = [];
  let asks = false;
  for (const c of cards) {
    if (!c.text.trim()) out.push({ card: c.id, code: "text" });
    if (c.kind === "quiz") {
      asks = true;
      const opts = c.options ?? [];
      if (opts.length !== 3 || opts.some((o) => !o.trim())) out.push({ card: c.id, code: "options" });
      else if (!CHOICES.includes((c.correct ?? "") as (typeof CHOICES)[number])) out.push({ card: c.id, code: "correct" });
    } else if (c.kind === "answer") {
      asks = true;
      if (!(c.answers ?? []).some((a) => a.trim())) out.push({ card: c.id, code: "answers" });
    }
  }
  if (cards.length === 0) out.push({ code: "empty" });
  else if (!asks) out.push({ code: "no_question" });
  return out;
}

/** Each card's number: the questions counted in order, info cards 0. */
export function numbers(cards: Card[]): number[] {
  let n = 0;
  return cards.map((c) => (c.kind === "info" ? 0 : ++n));
}

/** cards with the one at from moved to to. */
export function move<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list;
  const out = [...list];
  const [it] = out.splice(from, 1);
  out.splice(to, 0, it!);
  return out;
}
