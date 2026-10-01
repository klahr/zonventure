import { useCallback, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Bot } from "lucide-react";
import { useT } from "../i18n.tsx";
import { initials } from "./kit.tsx";
import type { WatchMessage } from "./live.ts";

/** ws.Peer: someone with the page open, or an assistant editing it lately. */
export interface Peer {
  id: string;
  user_id: number;
  name: string;
  kind: "human" | "assistant";
  /** What they have open; in the designer, a step — since when. */
  focus?: string;
  focus_since?: string;
  /** The steps an assistant's last save changed. */
  touched?: string[];
  /** What an assistant said it is doing. */
  note?: string;
  /** Their picture, absent while they have none. */
  avatar?: string;
  since: string;
}

/** Where a peer points: a spot in flow coordinates, and what they look at. */
export interface Pointer {
  x?: number | null;
  y?: number | null;
  /** The centre of their view, in flow coordinates, and its zoom. */
  view?: { x: number; y: number; zoom: number };
  /** When it arrived, for fading out a pointer gone still. */
  t: number;
}

// Hues spread by the golden angle, so a handful of people all differ;
// an assistant's sits apart from its person's.
export function peerColor(p: Pick<Peer, "user_id" | "kind">): string {
  const hue = Math.round((p.user_id * 137.508 + (p.kind === "assistant" ? 180 : 0)) % 360);
  return `hsl(${hue} 70% 45%)`;
}

/**
 * Pointers change many times a second, so they live outside React state:
 * only what draws them subscribes, and the page around it stays still.
 */
export class PointerStore {
  private map = new Map<string, Pointer>();
  private listeners = new Set<() => void>();
  private snapshot: ReadonlyMap<string, Pointer> = this.map;
  subscribe = (l: () => void) => {
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  };
  get = (): ReadonlyMap<string, Pointer> => this.snapshot;
  set(id: string, p: Pointer) {
    this.map.set(id, p);
    this.emit();
  }
  /** Forgets pointers of peers no longer present. */
  keep(ids: Set<string>) {
    let changed = false;
    for (const id of this.map.keys()) if (!ids.has(id)) changed = this.map.delete(id) || changed;
    if (changed) this.emit();
  }
  private emit() {
    this.snapshot = new Map(this.map);
    for (const l of this.listeners) l();
  }
}

export function usePointers(store: PointerStore): ReadonlyMap<string, Pointer> {
  return useSyncExternalStore(store.subscribe, store.get);
}

export interface Presence {
  /** This page's own peer id. */
  self: string | null;
  /** Everyone else, longest present first. */
  others: Peer[];
  /** This page's own peer, as the others see it. */
  me: Peer | null;
  pointers: PointerStore;
  /** Feeds a watch frame in; true when it was a presence frame. */
  take: (m: WatchMessage) => boolean;
}

/** Who else is here, from the presence frames of a watch. */
export function usePresence(): Presence {
  const [self, setSelf] = useState<string | null>(null);
  const [peers, setPeers] = useState<Peer[]>([]);
  const store = useRef<PointerStore>(null);
  store.current ??= new PointerStore();
  const selfRef = useRef(self);
  selfRef.current = self;
  const take = useCallback((m: WatchMessage) => {
    switch (m.kind) {
      case "presence_self":
        setSelf((m.data as { id: string }).id);
        return true;
      case "presence": {
        const ps = (m.data as { peers: Peer[] | null }).peers ?? [];
        setPeers(ps);
        store.current!.keep(new Set(ps.map((p) => p.id)));
        return true;
      }
      case "pointer": {
        const d = m.data as { id: string; at: Omit<Pointer, "t"> };
        if (d.id !== selfRef.current) store.current!.set(d.id, { ...d.at, t: Date.now() });
        return true;
      }
    }
    return false;
  }, []);
  const others = useMemo(() => peers.filter((p) => p.id !== self), [peers, self]);
  const me = peers.find((p) => p.id === self) ?? null;
  return { self, others, me, pointers: store.current, take };
}

/**
 * The people (and assistants) on a page, as a stack of avatars. Several
 * tabs of one person show as one. Clicking one follows them, when the
 * page can.
 */
export function PresenceBar({
  peers,
  following,
  onFollow,
  describe,
}: {
  peers: Peer[];
  following?: string | null;
  onFollow?: (p: Peer | null) => void;
  /** A line under the name: what they are on. */
  describe?: (p: Peer) => string;
}) {
  const t = useT();
  const shown = useMemo(() => {
    const seen = new Set<string>();
    return peers.filter((p) => {
      const k = `${p.kind}:${p.user_id}`;
      if (seen.has(k)) return false;
      seen.add(k);
      return true;
    });
  }, [peers]);
  if (!shown.length) return null;
  return (
    <div className="presence" role="list" aria-label={t.t("Here now")}>
      {shown.slice(0, 6).map((p) => {
        const line = describe?.(p);
        const title = (p.kind === "assistant" ? t.t("{{name}}'s assistant", { name: p.name }) : p.name) + (line ? `\n${line}` : "");
        const on = following === p.id;
        return (
          <button
            key={p.id}
            type="button"
            role="listitem"
            className={"presence__peer" + (p.kind === "assistant" ? " is-assistant" : "") + (on ? " is-following" : "")}
            style={{ "--peer": peerColor(p) } as React.CSSProperties}
            title={title}
            aria-label={title}
            aria-pressed={onFollow ? on : undefined}
            disabled={!onFollow}
            onClick={() => onFollow?.(on ? null : p)}
          >
            {p.kind === "assistant" ? <Bot size={14} aria-hidden /> : p.avatar ? <img src={p.avatar} alt="" /> : initials(p.name.replace(/@.*/, ""))}
          </button>
        );
      })}
      {shown.length > 6 && <span className="presence__more">+{shown.length - 6}</span>}
    </div>
  );
}

/**
 * Who holds what `me` has open: another person who opened it first.
 * Times are the server's, so two pages agree on who was first.
 */
export function holderOf(focus: string, me: Peer | null, others: Peer[]): Peer | null {
  const at = (p: Peer) => Date.parse(p.focus_since ?? "") || 0;
  const mine = me?.focus === focus ? at(me) : null;
  const first = (p: Peer) => mine === null || at(p) < mine || (at(p) === mine && p.id < me!.id);
  return others.find((p) => p.kind === "human" && p.focus === focus && first(p)) ?? null;
}
