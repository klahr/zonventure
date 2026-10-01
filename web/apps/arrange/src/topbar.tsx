import { createContext, useContext, useEffect, type ReactNode } from "react";
import { createPortal } from "react-dom";

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

/**
 * A place in the top bar, beside the organisation, for what a page
 * shows about itself live — who else has it open. The page renders into
 * it through TopBarPortal while it is mounted.
 */
export const TopBarSlot = createContext<HTMLElement | null>(null);

export function TopBarPortal({ children }: { children: ReactNode }) {
  const slot = useContext(TopBarSlot);
  return slot ? createPortal(children, slot) : null;
}
