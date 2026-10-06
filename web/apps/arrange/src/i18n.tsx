import {
  createContext,
  useContext,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { I18n } from "@zonventure/i18n";
import { hostedSv } from "@zonventure/hosted";
import classic from "../../../../internal/server/web/locales/web/sv.json";
import sv from "./locales/sv.json";

/**
 * Catalogs.
 *
 * English is intentionally empty: the key is the English source
 * string, so t("Log in") returns "Log in" under en and an untranslated
 * Swedish key degrades to readable English instead of a bare
 * identifier.
 *
 * Swedish is three layers, later ones winning: the classic console's
 * catalog (same English keys, so a ported screen keeps its
 * translations), one file per ported area under locales/sv/ (so areas
 * are translated without touching each other's files), and this app's
 * own sv.json. The hosted views' strings (@zonventure/hosted) come first.
 */
const areas = import.meta.glob<Record<string, string>>("./locales/sv/*.json", { eager: true, import: "default" });
const catalogs = { en: {}, sv: { ...hostedSv, ...(classic as Record<string, string>), ...Object.assign({}, ...Object.values(areas)), ...sv } };

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
