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
  text: string;
}

/** Feedback for an action the engine accepted (HTTP 200). */
export function feedbackForResult(kind: play.PlayAction["kind"], r: play.Result, t: Translate): Feedback | null {
  switch (kind) {
    case "answer":
      if (r.accepted) {
        const pts = Math.round((r.points ?? 0) * 10) / 10;
        return { tone: "good", text: pts > 0 ? t("Correct! +{{points}} points", { points: pts }) : t("Correct!") };
      }
      // A wrong answer that took a pity path ("after 5 tries") moved the
      // team on; saying "try again" would be wrong.
      if (r.port) return { tone: "info", text: r.response || t("That wasn't it — moving on.") };
      if (r.response) return { tone: "bad", text: r.response };
      if (r.blocked_until) return { tone: "bad", text: t("Not right. Wait a moment before trying again.") };
      return { tone: "bad", text: t("That's not it. Try again.") };
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
    case "paused":
      return { tone: "info", text: t("The game is paused. Hold on.") };
    case "not_running":
      return { tone: "info", text: t("The game is not running.") };
    default:
      return { tone: "bad", text: t("Something went wrong. Try again.") };
  }
}
