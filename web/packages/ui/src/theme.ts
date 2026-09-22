// Theme selection, matching the mechanism the existing pages already
// use so a user's choice carries across the old and new surfaces
// during the migration: the key is `zv-theme` and the value lands on
// <html data-theme>.
//
// "system" is a real third state, not the absence of a choice. It
// removes the attribute entirely so the
// `:root:not([data-theme])` media-query branch in tokens.css takes
// over. Storing "system" explicitly lets a user go dark → system and
// have that stick, which clearing the key alone would not express.

export type Theme = "light" | "dark" | "system";

export const THEME_STORAGE_KEY = "zv-theme";

function isTheme(v: unknown): v is Theme {
  return v === "light" || v === "dark" || v === "system";
}

/** Read the stored preference. Defaults to "system". */
export function readTheme(): Theme {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    return isTheme(raw) ? raw : "system";
  } catch {
    // Storage access throws outright when cookies are blocked.
    return "system";
  }
}

/** Apply a theme to the document and persist it. */
export function setTheme(theme: Theme): void {
  applyTheme(theme);
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    /* private mode — the theme still applies for this session */
  }
}

/** Apply without persisting. Used by the pre-paint inline script. */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") {
    delete root.dataset.theme;
  } else {
    root.dataset.theme = theme;
  }
}

/** Resolve "system" to what the OS currently reports. */
export function resolvedTheme(theme: Theme = readTheme()): "light" | "dark" {
  if (theme !== "system") return theme;
  return globalThis.matchMedia?.("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

/**
 * The script every app inlines in <head>, before first paint.
 *
 * It must be inline and synchronous: a deferred or bundled script runs
 * after the first paint, so a dark-mode user sees a white flash. This
 * is the one script the CSP allows by hash — keep it byte-stable, or
 * remember to update the hash when it changes.
 */
export const THEME_BOOT_SCRIPT = `(function(){try{var t=localStorage.getItem(${JSON.stringify(
  THEME_STORAGE_KEY,
)});if(t==="light"||t==="dark"){document.documentElement.dataset.theme=t}}catch(e){}})();`;
