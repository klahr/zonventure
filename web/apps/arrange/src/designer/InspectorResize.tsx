import { useState, type RefObject } from "react";

const KEY = "zonventure.inspector.width";
const MIN = 280;
const DEFAULT = 320;
const MAX = 720;
const STEP = 16;

// Never so wide that the canvas beside it is squeezed out.
const maxWidth = () => Math.max(MIN, Math.min(MAX, Math.round(window.innerWidth * 0.6)));
const clamp = (w: number) => Math.min(Math.max(Math.round(w), MIN), maxWidth());

function saved(): number {
  try {
    const w = Number(localStorage.getItem(KEY));
    return w >= MIN && w <= MAX ? w : DEFAULT;
  } catch {
    return DEFAULT;
  }
}

/**
 * The inspector's width, dragged from a handle on its left edge.
 *
 * The width is --dz-inspector-w on `root`, which every rule that makes
 * room for the inspector already reads. While dragging it is written to
 * the element directly: a React render per pointer move would re-render
 * the whole designer. The width lands in state, and is remembered, on
 * release. Double-click restores the default.
 */
export function useInspectorWidth(root: RefObject<HTMLElement | null>, label: string) {
  const [width, setWidth] = useState(saved);

  const commit = (w: number) => {
    setWidth(w);
    try {
      localStorage.setItem(KEY, String(w));
    } catch {
      /* private mode: the width lasts this visit */
    }
  };

  const onPointerDown = (e: React.PointerEvent<HTMLElement>) => {
    const el = root.current;
    if (e.button !== 0 || !el) return;
    e.preventDefault();
    const handle = e.currentTarget;
    const start = { x: e.clientX, w: clamp(width) };
    let w = start.w;
    handle.setPointerCapture(e.pointerId);
    el.dataset.resizing = "true";
    const move = (ev: PointerEvent) => {
      w = clamp(start.w + start.x - ev.clientX);
      el.style.setProperty("--dz-inspector-w", w + "px");
    };
    // On the window, so the drag follows the pointer with or without the
    // capture; any of release, cancel or a lost capture ends it, once.
    const end = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
      handle.removeEventListener("lostpointercapture", end);
      if (!el.dataset.resizing) return;
      delete el.dataset.resizing;
      commit(w);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
    handle.addEventListener("lostpointercapture", end);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    const w = clamp(width);
    const next =
      e.key === "ArrowLeft" ? w + STEP : e.key === "ArrowRight" ? w - STEP : e.key === "Home" ? MIN : e.key === "End" ? maxWidth() : null;
    if (next === null) return;
    e.preventDefault();
    commit(clamp(next));
  };

  const current = clamp(width);
  return {
    style: { "--dz-inspector-w": current + "px" } as React.CSSProperties,
    handle: (
      <div
        className="inspector-resize"
        role="separator"
        aria-orientation="vertical"
        aria-label={label}
        aria-valuemin={MIN}
        aria-valuemax={maxWidth()}
        aria-valuenow={current}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onKeyDown={onKeyDown}
        onDoubleClick={() => commit(DEFAULT)}
      />
    ),
  };
}
