import { useRef, useState, type FormEvent } from "react";
import { ApiError, play } from "@zonventure/api";
import { Sheet, type Translate } from "@zonventure/play-ui";

/** Taps on the title that open the cheat sheet, and how quickly they must come. */
const TAPS = 7;
const TAP_WINDOW_MS = 3000;

/** A tap handler that calls open on the seventh quick tap. */
export function useSevenTaps(open: () => void): () => void {
  const taps = useRef<number[]>([]);
  return () => {
    const now = Date.now();
    taps.current = [...taps.current.filter((at) => now - at < TAP_WINDOW_MS), now];
    if (taps.current.length >= TAPS) {
      taps.current = [];
      open();
    }
  };
}

/**
 * The cheat password, for a tester. The server checks it here and again
 * on every cheat; the phone keeps it for this browser session only, so
 * closing the tab ends cheating.
 */
export function CheatSheet({ t, onClose, onUnlocked }: { t: Translate; onClose: () => void; onUnlocked: (password: string) => void }) {
  const [password, setPassword] = useState("");
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!password) return;
    setChecking(true);
    setError(null);
    try {
      await play.unlockCheat(password);
      onUnlocked(password);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 403
          ? t("Wrong password.")
          : err instanceof ApiError && err.status === 429
            ? t("That was a lot of tries. Wait a minute, then try again.")
            : t("Couldn't reach the server. Check your connection and try again."),
      );
    } finally {
      setChecking(false);
    }
  };

  return (
    <div className="pv app-inbox__pv">
      <Sheet title={t("Cheat")} t={t} onClose={onClose}>
        <form className="pv-sheet__body app-menu__field" onSubmit={(e) => void submit(e)}>
          <label className="app-menu__label" htmlFor="cheat-password">{t("Password")}</label>
          <div className="app-menu__row">
            <input
              id="cheat-password"
              className="pv-input"
              type="password"
              value={password}
              autoComplete="off"
              autoFocus
              onChange={(e) => setPassword(e.target.value)}
            />
            <button type="submit" className="pv-btn pv-btn--primary" disabled={!password || checking}>
              {t("Unlock")}
            </button>
          </div>
          <span className="app-menu__hint">
            {error ? <span className="app-menu__error">{error}</span> : t("For testing. Every answer counts as right, for no points, and the team is left out of the rankings.")}
          </span>
        </form>
      </Sheet>
    </div>
  );
}
