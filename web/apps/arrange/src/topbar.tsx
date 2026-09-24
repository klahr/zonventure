import { createContext, useContext, useEffect } from "react";

/**
 * The shell's top bar title, as in dazyflow: the product wordmark,
 * or the name of the adventure open in the designer. A page claims the
 * title while it is mounted; null gives the wordmark back.
 */
export const TopBarTitle = createContext<(title: string | null) => void>(() => {});

export function useTopBarTitle(title: string | null) {
  const set = useContext(TopBarTitle);
  useEffect(() => {
    set(title);
    return () => set(null);
  }, [set, title]);
}
