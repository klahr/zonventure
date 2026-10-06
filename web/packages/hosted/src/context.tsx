import { createContext, useContext, useSyncExternalStore, type ReactNode } from "react";
import type { I18n } from "@zonventure/i18n";
import type { HostedApi } from "./api.ts";

/**
 * What the hosted views need from the app they sit in: its I18n (whose
 * Swedish catalog includes this package's sv.json) and the API to call.
 * run wraps an action with the page's busy state and error handling, and
 * reloads the event when it is done.
 */
export interface Hosted {
  i18n: I18n;
  api: HostedApi;
  run: (fn: () => Promise<unknown>) => Promise<void>;
  busy: boolean;
}

const Ctx = createContext<Hosted | null>(null);

export function HostedProvider({ value, children }: { value: Hosted; children: ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useHosted(): Hosted {
  const h = useContext(Ctx);
  if (!h) throw new Error("useHosted outside HostedProvider");
  useSyncExternalStore(
    (cb) => h.i18n.subscribe(cb),
    () => h.i18n.current(),
    () => h.i18n.current(),
  );
  return h;
}
