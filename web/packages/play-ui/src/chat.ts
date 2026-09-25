/**
 * How long a chat shows someone typing before a message: a fast texter's
 * 35 ms a character after a moment's pause, so a long message takes
 * longer than a short one — up to 7 s, so a paragraph does not stall
 * the game.
 */
export function typingTime(text: string): number {
  return Math.min(7000, 400 + [...text].length * 35);
}
