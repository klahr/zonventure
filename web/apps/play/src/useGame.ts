import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, NetworkError, play } from "@zonventure/api";
import { Outbox, PENDING_ANSWERS_MAX, clientActionId, type PendingAction } from "@zonventure/outbox";
import { feedbackForRefusal, feedbackForResult, type Feedback, type Translate } from "@zonventure/play-ui";
import { downscalePhoto } from "./downscale.ts";

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
export type GameStatus = "loading" | "signed-out" | "ended" | "ready" | "error";

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
  /**
   * Sign this phone out, or with erase, delete the player's play data and
   * revoke their link. Either way this phone forgets the game. Throws
   * when the server can't be reached, so nothing is forgotten half-way.
   */
  leave: (erase: boolean) => Promise<void>;
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
    if (!raw) return null;
    const s = JSON.parse(raw) as play.PlayState & { saved_at?: number };
    // The view's clock is the engine's as of when it was saved; move it
    // on by the time since, so countdowns are right before a reload lands.
    if (s.view?.now && typeof s.saved_at === "number") {
      const t = Date.parse(s.view.now) + Math.max(0, Date.now() - s.saved_at);
      if (Number.isFinite(t)) s.view = { ...s.view, now: new Date(t).toISOString() };
    }
    return s;
  } catch {
    return null;
  }
}

function saveLast(s: play.PlayState | null) {
  try {
    if (s) localStorage.setItem(LAST_KEY, JSON.stringify({ ...s, saved_at: Date.now() }));
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

/**
 * The team this tab plays for, as of the last load. Queued actions carry
 * it, so a phone that opens another team's link never sends the first
 * team's queued answer under the second team's session.
 */
let currentTeam: number | null = null;

/**
 * Refusals that settle a queued action: the server has decided, and
 * sending it again can't change that. Anything else — a server error, a
 * rate limit, a paused game — keeps the action queued for the next try.
 */
const SETTLED = new Set(["stale", "finished", "wrong_node", "not_allowed", "unanswered", "time_up", "not_here", "no_fix", "no_game", "unknown_action", "too_long"]);

function settles(e: ApiError): boolean {
  if (e.code) return SETTLED.has(e.code);
  return e.status >= 400 && e.status < 500 && e.status !== 408 && e.status !== 429;
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
  const viewRef = useRef(state?.view);
  viewRef.current = state?.view;
  // Orders what reaches the screen: a reload that started before an
  // action's response was shown must not put the older view back.
  const seq = useRef({ next: 0, applied: 0 });

  const showView = useCallback((view: play.View | undefined) => {
    if (!view) return;
    seq.current.applied = ++seq.current.next;
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
    const mine = ++seq.current.next;
    try {
      const s = await play.getPlay();
      currentTeam = s.team.id;
      if (seq.current.applied > mine) return;
      seq.current.applied = mine;
      // A pushed move (a teammate's answer, arriving at the place) makes
      // the last answer's "Correct!" stale; drop it once the step or its
      // arrival state changes.
      const prev = viewRef.current;
      // Only from a step the player could act on: leaving a start or a
      // place the team was walking to is the team's own arrival.
      const working = !!prev && (prev.type === "puzzle" || prev.type === "story") && !(prev.zone && !prev.here);
      if (working && prev?.node && s.view?.node && prev.node !== s.view.node && !s.view.finished) {
        // A teammate answered: say so, or the step just swaps under the
        // player (and whatever they were typing is gone).
        setFeedback({ tone: "info", text: tRef.current("Your team moved on. Here's the next step.") });
      } else if (prev?.node !== s.view?.node || prev?.here !== s.view?.here) {
        setFeedback(null);
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
        const team = entry.data["team"];
        if (!id || !action || (typeof team === "number" && currentTeam !== null && team !== currentTeam)) {
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
          // A passing refusal (server trouble, a paused game) keeps the
          // action, and the ones after it, for the next try.
          if (e instanceof ApiError && !settles(e)) break;
          if (e instanceof ApiError) showView(refusalView(e));
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
      const queue = (): boolean => {
        const waiting = playEntries();
        const same = action.kind === "answer" && waiting.some((p) => {
          const a = p.data["action"] as play.PlayAction | undefined;
          return a?.kind === "answer" && a.node === action.node;
        });
        if (same || waiting.length >= PENDING_ANSWERS_MAX) {
          setFeedback({
            tone: "info",
            text: same
              ? tRef.current("Your answer is waiting to send. We'll send it as soon as you're back online.")
              : tRef.current("Several moves are waiting to send. Wait until you're back online."),
          });
          return false;
        }
        outbox.enqueue({ kind: "play", data: { client_action_id: id, action, ...(currentTeam !== null ? { team: currentTeam } : {}) } });
        setQueued(playEntries().length);
        return true;
      };
      setBusy(true);
      setFeedback(null);
      // Something already waiting goes first, so this can't overtake it.
      // A cheat carries its password, so it is never queued (the queue
      // is kept in the phone's storage): offline, it just fails.
      if (playEntries().length > 0 && action.kind !== "timeout" && action.kind !== "cheat") {
        if (queue()) {
          await flush();
          if (outbox.pending.some((p) => p.data.client_action_id === id)) {
            setFeedback({ tone: "info", text: tRef.current("No connection. We'll send it as soon as you're back online.") });
          }
        }
        setBusy(false);
        return;
      }
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
          if (action.kind === "cheat") {
            setFeedback({ tone: "bad", text: tRef.current("No connection. Try again when you're back online.") });
          } else if (action.kind !== "timeout" && queue()) {
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
    [fail, showView, flush],
  );

  // A photo is not queued: it is large, and the player sees at once
  // whether it arrived and can simply take it again.
  const photo = useCallback(
    async (node: string, block: string, file: File) => {
      try {
        await play.uploadPhoto(node, block, await downscalePhoto(file));
      } catch (e) {
        fail(e);
        throw e;
      }
      await refresh();
    },
    [fail, refresh],
  );

  const leave = useCallback(async (erase: boolean) => {
    await (erase ? play.forget() : play.logout());
    outbox.pending.length = 0;
    outbox.persist();
    setQueued(0);
    saveLast(null);
    currentTeam = null;
    setState(null);
    toWorker({ type: "forget" });
    setStatus("signed-out");
  }, []);

  // "Correct!" is a moment, not a state: it fades, so it doesn't sit on
  // the next step while that one waits for the team to reach a place.
  useEffect(() => {
    if (feedback?.tone !== "good") return;
    const id = setTimeout(() => setFeedback((f) => (f === feedback ? null : f)), 5_000);
    return () => clearTimeout(id);
  }, [feedback]);

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
    leave,
  };
}

/** The current view carried by a refusal body, if any. */
function refusalView(e: ApiError): play.View | undefined {
  const b = e.body as { result?: play.Result } | undefined;
  return b?.result?.view;
}
