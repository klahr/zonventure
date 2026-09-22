import {
  createContext,
  useContext,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { I18n } from "@zonventure/i18n";
import sv from "./locales/sv.json";

/**
 * Catalogs.
 *
 * English is intentionally empty: the key is the English source
 * string, so t("Log in") returns "Log in" under en and an untranslated
 * Swedish key degrades to readable English instead of a bare
 * identifier.
 *
 * Phase 1 carries only the shell strings. The full 2,116-line
 * catalogs in internal/server/web/locales/ come across as their
 * surfaces are ported, so the two never disagree about a string that
 * is live in both places.
 */
const catalogs = { en: {}, sv };

const instance = new I18n(catalogs);
const I18nContext = createContext(instance);

export function I18nProvider({ children }: { children: ReactNode }) {
  return (
    <I18nContext.Provider value={instance}>{children}</I18nContext.Provider>
  );
}

/**
 * Subscribe to the active locale.
 *
 * useSyncExternalStore rather than state in a provider: the catalog
 * lives outside React, and this re-renders consumers on a locale
 * change without a reload. The old runtime called location.reload(),
 * which is the last thing you want mid-event on a bad connection.
 */
export function useT(): I18n {
  const i18n = useContext(I18nContext);
  useSyncExternalStore(
    (cb) => i18n.subscribe(cb),
    () => i18n.current(),
    () => i18n.current(),
  );
  return i18n;
}
