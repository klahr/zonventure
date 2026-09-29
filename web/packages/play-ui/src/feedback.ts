// What to tell the player after an action. Shared by the player app and
// the designer's simulator so an author sees the exact words a team will.

import type { play } from "@zonventure/api";

/**
 * A translator: the English source string is the key, {{name}} is
 * interpolated. Both apps pass their @zonventure/i18n instance's `t`.
 */
export type Translate = (key: string, vars?: Record<string, string | number>) => string;

/** Identity translation with interpolation, for callers without a catalog. */
export const english: Translate = (key, vars) =>
  vars ? key.replace(/\{\{(\w+)\}\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : key;

export interface Feedback {
  tone: "good" | "bad" | "info";
  /** The line that stays on screen. */
  text: string;
  /**
   * A moment to mark, as play.html did: a confetti celebration for a
   * right answer, a quick shake for a wrong one. Played over the step,
   * then gone; `text` stays.
   */
  burst?: { kind: "celebrate" | "rebuff"; head: string; sub?: string };
}

/** Feedback for an action the engine accepted (HTTP 200). */
export function feedbackForResult(kind: play.PlayAction["kind"], r: play.Result, t: Translate): Feedback | null {
  switch (kind) {
    case "answer":
      if (r.accepted) {
        const pts = Math.round((r.points ?? 0) * 10) / 10;
        const sub = pts > 0 ? t("+{{points}} points", { points: pts }) : undefined;
        return {
          tone: "good",
          text: pts > 0 ? t("Correct! +{{points}} points", { points: pts }) : t("Correct!"),
          burst: { kind: "celebrate", head: t("Correct!"), ...(sub ? { sub } : {}) },
        };
      }
      // A wrong answer that took a pity path ("after 5 tries") moved the
      // team on; saying "try again" would be wrong.
      if (r.port) return { tone: "info", text: r.response || t("That wasn't it — moving on.") };
      {
        const text = r.response || (r.blocked_until ? t("Not right. Wait a moment before trying again.") : t("That's not it. Try again."));
        return {
          tone: "bad",
          text,
          burst: { kind: "rebuff", head: t("Not quite"), ...(r.response ? { sub: r.response } : {}) },
        };
      }
    case "skip":
      return { tone: "info", text: t("Skipped.") };
    case "timeout":
      return { tone: "info", text: t("Time ran out.") };
    default:
      return null;
  }
}

/** Feedback for a refused action, by its stable code. */
export function feedbackForRefusal(code: string | undefined, t: Translate): Feedback {
  switch (code) {
    case "stale":
      return { tone: "info", text: t("Your team has already moved on. Here is where you are now.") };
    case "not_here":
      return { tone: "bad", text: t("You need to be at the place first.") };
    case "locked":
      return { tone: "bad", text: t("Wait a moment before trying again.") };
    case "time_up":
      return { tone: "bad", text: t("Time is up for this one.") };
    case "not_allowed":
      return { tone: "bad", text: t("That isn't possible yet.") };
    case "finished":
      return { tone: "info", text: t("Your team has finished.") };
    case "unanswered":
      return { tone: "bad", text: t("Answer the questions on this page first.") };
    case "paused":
      return { tone: "info", text: t("The game is paused. Hold on.") };
    case "not_running":
      return { tone: "info", text: t("The game is not running.") };
    case "rate_limited":
      return { tone: "bad", text: t("That was a lot of tries. Wait a minute, then try again.") };
    case "too_long":
      return { tone: "bad", text: t("That's too long. Shorten it and try again.") };
    case "no_game":
      return { tone: "info", text: t("The game hasn't started for your team yet.") };
    case "wrong_node":
    case "unknown_action":
      return { tone: "info", text: t("That doesn't work here. Here is where you are now.") };
    default:
      return { tone: "bad", text: t("Something went wrong. Try again.") };
  }
}
