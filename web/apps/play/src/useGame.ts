import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, NetworkError, play } from "@zonventure/api";
import { Outbox, clientActionId, type PendingAction } from "@zonventure/outbox";
import { feedbackForRefusal, feedbackForResult, type Feedback, type Translate } from "@zonventure/play-ui";

/**
 * The game loop: load the team's state, send actions, and keep going
 * through bad signal.
 *
 * The rules this holds to, because a puzzle hunt is played outdoors on
 * mediocre phones:
 *
 *   - The screen only ever shows a view the server sent. An action never
 *     advances the UI on its own; the response does. Guessing ahead would
 *     show a team the next clue before it has earned it.
 *   - An action that cannot reach the server is queued in the outbox,
 *     with the id it was first sent under, and re-sent when the network
 *     returns. The server replays its first response for a repeated id,
 *     so an answer that did land before the connection dropped is not
 *     counted twice.
 *   - A queued action the server refuses when it finally arrives — the
 *     team moved on meanwhile — is dropped, and the refusal carries the
 *     view to show instead.
 */

/**
 * "ended" is a finished team whose session the server has since closed —
 * it ends every player session when the event completes. The team keeps
 * its final screen, read-only, instead of being told to sign in again.
 */
export type GameStatus = "loading" | "signed-out" | "ended" | "lanes" | "ready" | "error";

export interface Game {
  status: GameStatus;
  state: play.PlayState | null;
  busy: boolean;
  feedback: Feedback | null;
  /** Actions waiting for the network. */
  queued: number;
  online: boolean;
  act: (a: play.PlayAction) => void;
  /** Sends the team's picture for a photo block, then reloads. Throws when it does not arrive. */
  photo: (node: string, block: string, file: File) => Promise<void>;
  refresh: () => void;
  /** Send anything queued, then reload. */
  retry: () => void;
}

/** How often to retry the outbox while something is queued. */
const RETRY_MS = 15_000;

const outbox = new Outbox();
// load() appends, so it must run once per page even though React may
// mount the hook more than once (StrictMode does, in development).
let outboxLoaded = false;

/**
 * The last state the server sent, so a tab reopened in a dead spot shows
 * the step the team is on instead of an error. It is only ever a
 * server-sent view — never a guessed one — and it is replaced by the
 * first successful load. Dropped on sign-out, so the next person on a
 * shared phone does not see it.
 */
const LAST_KEY = "zv-play-last-v1";

function loadLast(): play.PlayState | null {
  try {
    const raw = localStorage.getItem(LAST_KEY);
    return raw ? (JSON.parse(raw) as play.PlayState) : null;
  } catch {
    return null;
  }
}

function saveLast(s: play.PlayState | null) {
  try {
    if (s) localStorage.setItem(LAST_KEY, JSON.stringify(s));
    else localStorage.removeItem(LAST_KEY);
  } catch {
    /* private mode / full: best effort */
  }
}

/**
 * Tell the service worker which files this team may keep offline (see
 * public/sw.js). Sent on every load; the worker skips what it has.
 */
function toWorker(msg: { type: "prefetch"; team: number; urls: string[] } | { type: "forget" }) {
  navigator.serviceWorker?.ready.then((r) => r.active?.postMessage(msg)).catch(() => {});
}

function playEntries(): PendingAction[] {
  return outbox.pending.filter((p) => p.kind === "play");
}

export function useGame(t: Translate): Game {
  const [state, setState] = useState<play.PlayState | null>(loadLast);
  const [status, setStatus] = useState<GameStatus>(state ? "ready" : "loading");
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const [queued, setQueued] = useState(0);
  const [online, setOnline] = useState(() => navigator.onLine);
  const tRef = useRef(t);
  tRef.current = t;

  const showView = useCallback((view: play.View | undefined) => {
    if (!view) return;
    setState((s) => {
      const next = s ? { ...s, view } : s;
      saveLast(next);
      return next;
    });
  }, []);

  const fail = useCallback((e: unknown) => {
    if (e instanceof ApiError && e.isUnauthorized) {
      // Nothing queued can be sent any more.
      outbox.pending.length = 0;
      outbox.persist();
      setQueued(0);
      const last = loadLast();
      if (last?.view?.finished) {
        setState(last);
        setStatus("ended");
        return true;
      }
      saveLast(null);
      setState(null);
      toWorker({ type: "forget" });
      setStatus("signed-out");
      return true;
    }
    return false;
  }, []);

  const refresh = useCallback(async () => {
    try {
      const s = await play.getPlay();
      if (s.engine === "lanes") {
        setStatus("lanes");
        return;
      }
      setState(s);
      saveLast(s);
      if (s.prefetch?.length) toWorker({ type: "prefetch", team: s.team.id, urls: s.prefetch.map(play.mediaURL) });
      setStatus("ready");
      setOnline(true);
    } catch (e) {
      if (fail(e)) return;
      if (e instanceof NetworkError) {
        setOnline(false);
        // Keep showing what we had; only a first load with nothing to
        // show is an error screen.
        setStatus((st) => (st === "loading" ? "error" : st));
        return;
      }
      setStatus("error");
    }
  }, [fail]);

  // Send whatever is queued, oldest first, stopping at the first network
  // failure so order is kept.
  const flushing = useRef(false);
  const flush = useCallback(async () => {
    if (flushing.current) return;
    flushing.current = true;
    try {
      for (const entry of playEntries()) {
        const id = entry.data.client_action_id;
        const action = entry.data["action"] as play.PlayAction | undefined;
        if (!id || !action) {
          // Unsendable. ack() matches by id, which this may not have.
          const i = outbox.pending.indexOf(entry);
          if (i >= 0) outbox.pending.splice(i, 1);
          outbox.persist();
          continue;
        }
        try {
          const r = await play.sendAction(action, id);
          showView(r.view);
          setFeedback(feedbackForResult(action.kind, r, tRef.current));
        } catch (e) {
          if (e instanceof NetworkError) {
            setOnline(false);
            break;
          }
          if (fail(e)) break;
          if (e instanceof ApiError) showView(refusalView(e));
          // Any answer from the server is final for this action.
        }
        outbox.ack(id);
        outbox.persist();
        setOnline(true);
      }
    } finally {
      flushing.current = false;
      setQueued(playEntries().length);
    }
  }, [fail, showView]);

  const act = useCallback(
    async (action: play.PlayAction) => {
      const id = clientActionId();
      setBusy(true);
      setFeedback(null);
      try {
        const r = await play.sendAction(action, id);
        showView(r.view);
        setFeedback(feedbackForResult(action.kind, r, tRef.current));
        setOnline(true);
      } catch (e) {
        if (fail(e)) return;
        if (e instanceof NetworkError) {
          // Moves are queued; so are hints and skips, which a player
          // chose deliberately. A timeout is not: the server decides that
          // from its own clock, and the next load will show it.
          if (action.kind !== "timeout") {
            outbox.enqueue({ kind: "play", data: { client_action_id: id, action } });
            setQueued(playEntries().length);
            setFeedback({
              tone: "info",
              text: tRef.current("No connection. We'll send it as soon as you're back online."),
            });
          }
          setOnline(false);
        } else if (e instanceof ApiError) {
          showView(refusalView(e));
          setFeedback(feedbackForRefusal(e.code, tRef.current));
        } else {
          setFeedback(feedbackForRefusal(undefined, tRef.current));
        }
      } finally {
        setBusy(false);
      }
    },
    [fail, showView],
  );

  // A photo is not queued: it is large, and the player sees at once
  // whether it arrived and can simply take it again.
  const photo = useCallback(
    async (node: string, block: string, file: File) => {
      try {
        await play.uploadPhoto(node, block, file);
      } catch (e) {
        fail(e);
        throw e;
      }
      await refresh();
    },
    [fail, refresh],
  );

  // Boot: restore anything queued by a previous tab, load, then flush.
  useEffect(() => {
    if (!outboxLoaded) {
      outboxLoaded = true;
      outbox.load();
    }
    setQueued(playEntries().length);
    void refresh().then(flush);
  }, [refresh, flush]);

  // Back online: flush first so the queued answer lands before we
  // re-read the state it changed.
  useEffect(() => {
    const on = () => {
      setOnline(true);
      void flush().then(refresh);
    };
    const off = () => setOnline(false);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, [flush, refresh]);

  // The online event is unreliable on mobile; retry on a timer too.
  useEffect(() => {
    if (queued === 0) return;
    const id = setInterval(() => void flush(), RETRY_MS);
    return () => clearInterval(id);
  }, [queued, flush]);

  // A tab coming back to the foreground may have missed pushes.
  useEffect(() => {
    const vis = () => {
      if (document.visibilityState === "visible") void flush().then(refresh);
    };
    document.addEventListener("visibilitychange", vis);
    return () => document.removeEventListener("visibilitychange", vis);
  }, [flush, refresh]);

  return {
    status,
    state,
    busy,
    feedback,
    queued,
    online,
    act: (a) => void act(a),
    photo,
    refresh: () => void refresh(),
    retry: () => void flush().then(refresh),
  };
}

/** The current view carried by a refusal body, if any. */
function refusalView(e: ApiError): play.View | undefined {
  const b = e.body as { result?: play.Result } | undefined;
  return b?.result?.view;
}
