import { useEffect, useRef, type RefObject } from "react";
import { ViewportPortal, useOnViewportChange, useReactFlow, useViewport } from "@xyflow/react";
import { Bot } from "lucide-react";
import { peerColor, usePointers, type Peer, type PointerStore } from "../ui/presence.tsx";

/** What the canvas needs to show others at work, and to show them this page. */
export interface CanvasLive {
  /** Everyone else on the adventure. */
  peers: Peer[];
  pointers: PointerStore;
  send: (kind: string, data: unknown) => void;
  /** The peer whose view this canvas follows. */
  following: string | null;
  /** Glide to a step, when seq changes — following an assistant. */
  flyTo: { slug: string; seq: number } | null;
  /** The author moved the canvas themselves: stop following. */
  onUserMove: () => void;
}

/** How often this page reports its pointer and view. */
const REPORT_MS = 60;
/** A pointer gone still this long fades out. */
const STILL_MS = 12_000;

/**
 * Drawn inside React Flow: everyone else's cursor where they point, a
 * badge gliding to the step an assistant works on, and — while
 * following someone — this canvas's view kept on theirs. It also
 * reports this page's own pointer and view, but only while anyone is
 * there to see them.
 */
export function LiveLayer({ live, wrapper }: { live: CanvasLive; wrapper: RefObject<HTMLDivElement | null> }) {
  const rf = useReactFlow();
  const { zoom } = useViewport();
  const pointers = usePointers(live.pointers);
  const watched = live.peers.length > 0;

  // --- this page's pointer and view, throttled ------------------------------------
  const last = useRef<{ x: number | null; y: number | null }>({ x: null, y: null });
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const sendRef = useRef(live.send);
  sendRef.current = live.send;
  const report = useRef(() => {});
  report.current = () => {
    if (timer.current) return;
    timer.current = setTimeout(() => {
      timer.current = null;
      const r = wrapper.current?.getBoundingClientRect();
      if (!r) return;
      const c = rf.screenToFlowPosition({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
      sendRef.current("pointer", { ...last.current, view: { x: Math.round(c.x), y: Math.round(c.y), zoom: rf.getZoom() } });
    }, REPORT_MS);
  };
  useEffect(() => {
    const el = wrapper.current;
    if (!el || !watched) return;
    const move = (e: PointerEvent) => {
      const p = rf.screenToFlowPosition({ x: e.clientX, y: e.clientY });
      last.current = { x: Math.round(p.x), y: Math.round(p.y) };
      report.current();
    };
    const leave = () => {
      last.current = { x: null, y: null };
      report.current();
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerleave", leave);
    report.current();
    return () => {
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerleave", leave);
    };
  }, [wrapper, watched, rf]);
  useOnViewportChange({ onChange: () => watched && report.current() });
  useEffect(() => () => void (timer.current && clearTimeout(timer.current)), []);

  // --- following -------------------------------------------------------------------
  const target = live.following ? pointers.get(live.following)?.view : undefined;
  useEffect(() => {
    if (target) void rf.setCenter(target.x, target.y, { zoom: target.zoom, duration: 350 });
  }, [target?.x, target?.y, target?.zoom, rf]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!live.flyTo) return;
    const n = rf.getInternalNode(live.flyTo.slug);
    if (!n) return;
    const w = n.measured.width ?? 240;
    const h = n.measured.height ?? 120;
    void rf.setCenter(n.internals.positionAbsolute.x + w / 2, n.internals.positionAbsolute.y + h / 2, {
      zoom: Math.max(rf.getZoom(), 0.8),
      duration: 700,
    });
  }, [live.flyTo?.seq]); // eslint-disable-line react-hooks/exhaustive-deps

  // --- others ------------------------------------------------------------------------
  const now = Date.now();
  const scale = `scale(${1 / zoom})`;
  return (
    <ViewportPortal>
      {live.peers.map((p) => {
        const color = peerColor(p);
        if (p.kind === "assistant") {
          const n = p.focus ? rf.getInternalNode(p.focus) : undefined;
          if (!n) return null;
          const x = n.internals.positionAbsolute.x + (n.measured.width ?? 240);
          const y = n.internals.positionAbsolute.y;
          return (
            <div key={p.id} className="live-bot" style={{ transform: `translate(${x}px, ${y}px)`, "--peer": color } as React.CSSProperties}>
              <div className="live-bot__badge" style={{ transform: `translate(-50%, -60%) ${scale}` }}>
                <Bot size={16} aria-hidden />
                {p.note && <span className="live-bot__note">{p.note}</span>}
              </div>
            </div>
          );
        }
        const at = pointers.get(p.id);
        if (at?.x == null || at.y == null) return null;
        const still = now - at.t > STILL_MS;
        return (
          <div
            key={p.id}
            className={"live-cursor" + (still ? " is-still" : "")}
            style={{ transform: `translate(${at.x}px, ${at.y}px)`, "--peer": color } as React.CSSProperties}
            aria-hidden
          >
            <div style={{ transform: scale, transformOrigin: "0 0" }}>
              <svg width="18" height="18" viewBox="0 0 18 18" className="live-cursor__arrow">
                <path d="M1 1 L1 15 L5 11 L8 17 L10.5 16 L7.5 10 L13 10 Z" />
              </svg>
              <span className="live-cursor__name">{p.name.replace(/@.*/, "")}</span>
            </div>
          </div>
        );
      })}
    </ViewportPortal>
  );
}
