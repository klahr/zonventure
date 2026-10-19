import { useEffect, useState } from "react";
import type { Translate } from "@zonventure/play-ui";

// Older Safari (iPad) has only the prefixed API.
type WebkitDocument = Document & { webkitFullscreenEnabled?: boolean; webkitFullscreenElement?: Element | null; webkitExitFullscreen?: () => void };
type WebkitElement = HTMLElement & { webkitRequestFullscreen?: () => void };

const doc = document as WebkitDocument;
const supported = !!(doc.fullscreenEnabled || doc.webkitFullscreenEnabled);
const current = () => !!(doc.fullscreenElement || doc.webkitFullscreenElement);

/**
 * Full screen on and off, at the bar's right end: the browser's own bars
 * gone, more of the step and map in sight outdoors. Absent where the
 * browser can't (iPhone Safari) or the app is already installed full screen.
 */
export function Fullscreen({ t }: { t: Translate }) {
  const [on, setOn] = useState(current);
  useEffect(() => {
    const sync = () => setOn(current());
    document.addEventListener("fullscreenchange", sync);
    document.addEventListener("webkitfullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
      document.removeEventListener("webkitfullscreenchange", sync);
    };
  }, []);
  // The page's own full screen also matches display-mode: fullscreen, so
  // only an installed app's counts, or the way back out would vanish.
  if (!supported || (!on && matchMedia("(display-mode: fullscreen), (display-mode: standalone)").matches)) return null;

  const toggle = () => {
    const el = document.documentElement as WebkitElement;
    try {
      if (current()) {
        if (doc.exitFullscreen) void doc.exitFullscreen().catch(() => {});
        else doc.webkitExitFullscreen?.();
      } else if (el.requestFullscreen) {
        void el.requestFullscreen({ navigationUI: "hide" }).catch(() => {});
      } else {
        el.webkitRequestFullscreen?.();
      }
    } catch {
      /* refused by the browser: the page stays as it was */
    }
  };

  return (
    <button type="button" className="app-fullscreen" onClick={toggle} aria-label={on ? t("Exit full screen") : t("Full screen")} aria-pressed={on}>
      <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        {on ? <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" /> : <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />}
      </svg>
    </button>
  );
}
