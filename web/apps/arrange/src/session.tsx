import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { ApiError, auth, type Whoami } from "@zonventure/api";

/**
 * Session state.
 *
 * "anonymous" is a normal state, not an error. /api/auth/whoami answers
 * 401 when there is no session, so an ApiError 401 here means "show the
 * login form", and only anything else is a genuine failure worth
 * surfacing. Conflating the two is how you end up showing "something
 * went wrong" to every logged-out visitor.
 */
export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: Whoami }
  | { status: "error"; message: string };

interface SessionContextValue {
  state: SessionState;
  /** Re-fetch whoami. Call after login, logout, or an org switch. */
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
}

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SessionState>({ status: "loading" });

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const user = await auth.whoami(signal);
      setState({ status: "authenticated", user });
    } catch (e) {
      if (signal?.aborted) return;
      if (e instanceof ApiError && e.isUnauthorized) {
        setState({ status: "anonymous" });
        return;
      }
      setState({
        status: "error",
        message: e instanceof Error ? e.message : String(e),
      });
    }
  }, []);

  useEffect(() => {
    const ac = new AbortController();
    void load(ac.signal);
    return () => ac.abort();
  }, [load]);

  const refresh = useCallback(() => load(), [load]);

  const logout = useCallback(async () => {
    try {
      await auth.logout();
    } catch {
      // Drop the session locally even if the call failed — the cookie
      // may already be gone, and leaving the user looking logged in
      // after they asked to leave is the worse outcome.
    }
    setState({ status: "anonymous" });
    // Leave for the site's front page, as people expect after logging
    // out. Where the landing page is off, "/" leads back to the login.
    location.assign("/");
  }, []);

  return (
    <SessionContext.Provider value={{ state, refresh, logout }}>
      {children}
    </SessionContext.Provider>
  );
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession used outside SessionProvider");
  return ctx;
}
