import { useEffect } from "react";
import type { play } from "@zonventure/api";
import { backdropVars, brandVars, loadFont } from "@zonventure/play-ui";

/**
 * Dresses the whole page in an adventure's look while it is shown — its
 * palette, typefaces and backdrop, as the play app draws them — so the
 * join, host and booking pages match the game the teams then play. The
 * properties go on the root, where the page's own background reads them.
 */
export function useLook(look: play.Brand | null | undefined) {
  useEffect(() => loadFont(look?.font), [look?.font]);
  useEffect(() => loadFont(look?.heading_font), [look?.heading_font]);
  const key = look ? JSON.stringify(look) : "";
  useEffect(() => {
    if (!look) return;
    const vars = { ...brandVars(look), ...backdropVars(look.backdrop, look) };
    const style = document.documentElement.style;
    for (const [k, v] of Object.entries(vars)) style.setProperty(k, v);
    return () => {
      for (const k of Object.keys(vars)) style.removeProperty(k);
    };
    // The look's content, not its identity: a reload hands a new object.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
}
