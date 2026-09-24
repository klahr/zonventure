import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { GripHorizontal, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { Button, ICON } from "./dz/ui.tsx";

interface Pos {
  x: number;
  y: number;
}

function load(key: string): Pos | null {
  try {
    const p = JSON.parse(localStorage.getItem(key) ?? "null") as Pos | null;
    return p && Number.isFinite(p.x) && Number.isFinite(p.y) ? p : null;
  } catch {
    return null;
  }
}

/**
 * A window that floats over the designer and is moved by its title bar.
 *
 * Used for the preview, so the phone can sit beside the step being
 * edited instead of taking the inspector's place. Where it was left is
 * remembered; it is kept on screen when the browser shrinks.
 */
export function FloatingWindow({ title, storageKey, onClose, actions, children }: { title: string; storageKey: string; onClose: () => void; actions?: ReactNode; children: ReactNode }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<Pos | null>(() => load(storageKey));

  // Keep at least the title bar reachable.
  const clamp = useCallback((p: Pos): Pos => {
    const el = box.current;
    const w = el?.offsetWidth ?? 400;
    return {
      x: Math.min(Math.max(p.x, 8 - w + 120), window.innerWidth - 120),
      y: Math.min(Math.max(p.y, 8), window.innerHeight - 48),
    };
  }, []);

  // First open: near the right edge, below the toolbar.
  useEffect(() => {
    if (pos) return;
    const w = box.current?.offsetWidth ?? 640;
    setPos({ x: Math.max(8, window.innerWidth - w - 24), y: 72 });
  }, [pos]);

  useEffect(() => {
    const onResize = () => setPos((p) => (p ? clamp(p) : p));
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [clamp]);

  const startDrag = (e: React.PointerEvent) => {
    if (e.button !== 0 || (e.target as HTMLElement).closest("button, select, input")) return;
    const start = { x: e.clientX, y: e.clientY, p: pos ?? { x: 0, y: 0 } };
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    const move = (ev: PointerEvent) => setPos(clamp({ x: start.p.x + ev.clientX - start.x, y: start.p.y + ev.clientY - start.y }));
    const up = () => {
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      setPos((p) => {
        if (p) {
          try {
            localStorage.setItem(storageKey, JSON.stringify(p));
          } catch {
            /* private mode: remembered for this visit only */
          }
        }
        return p;
      });
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
  };

  return (
    <div
      ref={box}
      className="float-win"
      role="dialog"
      aria-label={title}
      style={pos ? { left: pos.x, top: pos.y } : { visibility: "hidden" }}
    >
      <div className="float-win__bar" onPointerDown={startDrag}>
        <GripHorizontal size={ICON.sm} className="float-win__grip" aria-hidden />
        <span className="float-win__title">{title}</span>
        {actions}
        <Button variant="ghost" size="icon" onClick={onClose} aria-label={t.t("Close")} title={t.t("Close")}>
          <X size={ICON.md} />
        </Button>
      </div>
      <div className="float-win__body">{children}</div>
    </div>
  );
}
