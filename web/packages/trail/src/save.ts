import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "@zonventure/api";
import type { Card } from "./cards.ts";

/** A trail as the server keeps it: internal/admin/trails.go trailDTO. */
export interface TrailDoc {
  slug: string;
  title: string;
  language: string;
  template: string;
  kids: boolean;
  cards: Card[];
  revision: number;
  editable: boolean;
  published: boolean;
  unpublished: boolean;
  printed: boolean;
  reprint: boolean;
}

/** Saving a trail: the console's, or (later) a public draft's. */
export interface TrailApi {
  save(body: { title: string; cards: Card[]; revision: number }): Promise<TrailDoc>;
}

/** Where the trail stands with the server. */
export type SaveState = "saved" | "saving" | "unsaved" | "failed" | "conflict" | "designer";

const DELAY = 1000;

/**
 * Keeps a trail saved: every change is saved a second after the last
 * one, in order, one save at a time. A save refused because the trail
 * changed elsewhere ("changed") or in the designer ("designer") stops
 * saving: the page shows why and offers to load it again.
 */
export function useTrailSave(api: TrailApi, initial: TrailDoc) {
  const [doc, setDoc] = useState(initial);
  const [title, setTitle] = useState(initial.title);
  const [cards, setCards] = useState(initial.cards);
  const [state, setState] = useState<SaveState>(initial.editable ? "saved" : "designer");
  const revision = useRef(initial.revision);
  const dirty = useRef(false);
  const inFlight = useRef<Promise<void> | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const latest = useRef({ title, cards });
  latest.current = { title, cards };
  const stopped = state === "conflict" || state === "designer";

  const flush = useCallback(async (): Promise<boolean> => {
    clearTimeout(timer.current);
    if (inFlight.current) await inFlight.current;
    if (!dirty.current) return true;
    dirty.current = false;
    setState("saving");
    let ok = true;
    inFlight.current = (async () => {
      try {
        const saved = await api.save({ ...latest.current, revision: revision.current });
        revision.current = saved.revision;
        setDoc(saved);
        setState(dirty.current ? "unsaved" : "saved");
      } catch (e) {
        ok = false;
        dirty.current = true;
        if (e instanceof ApiError && e.status === 409) setState(e.code === "designer" ? "designer" : "conflict");
        else setState("failed");
      }
    })();
    await inFlight.current;
    inFlight.current = null;
    if (ok && dirty.current) return flush();
    return ok;
  }, [api]);

  const change = useCallback(
    (next: { title?: string; cards?: Card[] }) => {
      if (stopped) return;
      if (next.title !== undefined) setTitle(next.title);
      if (next.cards !== undefined) setCards(next.cards);
      dirty.current = true;
      setState("unsaved");
      clearTimeout(timer.current);
      timer.current = setTimeout(() => void flush(), DELAY);
    },
    [flush, stopped],
  );

  // Leaving with changes unsaved asks first.
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty.current) e.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => {
      window.removeEventListener("beforeunload", warn);
      clearTimeout(timer.current);
    };
  }, []);

  return { doc, title, cards, state, change, flush, readOnly: stopped };
}
