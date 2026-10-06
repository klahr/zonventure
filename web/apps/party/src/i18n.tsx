import {
  createContext,
  useContext,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { I18n } from "@zonventure/i18n";
import { hostedSv } from "@zonventure/hosted";
import sv from "./locales/sv.json";

/**
 * Catalogs. English is empty because the key is the English source
 * string. Swedish is the primary market, so every string a booker or
 * host reads has a translation here.
 */
const catalogs = { en: {}, sv: { ...hostedSv, ...sv } };

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
