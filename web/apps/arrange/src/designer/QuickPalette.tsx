import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  existingTargets,
  presetsFor,
  scorePreset,
  suggestionsFor,
  type DragFrom,
  type Graph,
  type Node,
  type Preset,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";

/**
 * The quick-add palette, modelled on dazyflow's QuickDropPalette.
 *
 * It opens where a dragged wire was released on empty canvas (or on a
 * double-click, or Ctrl+K), lists what can go there, and the pick is
 * created at that spot already wired in. Everything is reachable from the
 * keyboard: type to filter, arrows to move, Enter to pick, Escape to close.
 *
 * Behaviours carried over from dazyflow on purpose:
 *   - suggestions sit in their own group above the full list;
 *   - clicking the backdrop closes it;
 *   - the browser Back button closes it instead of leaving the designer,
 *     which is what a phone user or a mouse-back-button user expects of
 *     anything that looks like an overlay.
 */

export type PaletteChoice = { kind: "preset"; preset: Preset } | { kind: "existing"; node: Node };

interface Props {
  graph: Graph;
  from: DragFrom | null;
  /** Screen position to anchor near. */
  at: { x: number; y: number };
  /** Area to stay inside — the canvas, so the panel never covers the
   * inspector. Defaults to the window. */
  bounds?: DOMRect;
  onPick: (choice: PaletteChoice) => void;
  onClose: () => void;
}

interface Row {
  key: string;
  choice: PaletteChoice;
  label: string;
  hint: string;
  group: "suggested" | "all" | "existing";
}

let pendingHistoryPop: ReturnType<typeof setTimeout> | null = null;

export function QuickPalette({ graph, from, at, bounds, onPick, onClose }: Props) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Back button closes the palette. The history entry is pushed on open
  // and popped on close; the pop is deferred a tick so that closing one
  // palette and immediately opening another (pick, then drag again)
  // reuses the entry instead of fighting over it — the same trick
  // dazyflow uses.
  useEffect(() => {
    if (pendingHistoryPop) {
      clearTimeout(pendingHistoryPop);
      pendingHistoryPop = null;
    }
    if (!window.history.state?.zvPalette) {
      window.history.pushState({ zvPalette: true }, "");
    }
    const onPop = () => onCloseRef.current();
    window.addEventListener("popstate", onPop);
    return () => {
      window.removeEventListener("popstate", onPop);
      if (window.history.state?.zvPalette) {
        pendingHistoryPop = setTimeout(() => {
          pendingHistoryPop = null;
          if (window.history.state?.zvPalette) window.history.back();
        }, 0);
      }
    };
  }, []);

  const rows = useMemo<Row[]>(() => {
    const presets = presetsFor(from);
    const q = query.trim();

    const presetRow = (p: Preset, group: Row["group"]): Row => ({
      key: "p:" + p.id,
      choice: { kind: "preset", preset: p },
      label: t.t(p.label),
      hint: t.t(p.hint),
      group,
    });
    const existing = existingTargets(graph, from).map<Row>((n) => ({
      key: "n:" + n.slug,
      choice: { kind: "existing", node: n },
      label: n.title || n.slug,
      hint: t.t("Connect to this existing step") + ` · ${n.type}`,
      group: "existing",
    }));

    if (!q) {
      const sugIDs = suggestionsFor(from);
      const sug = sugIDs
        .map((id) => presets.find((p) => p.id === id))
        .filter((p): p is Preset => !!p)
        .map((p) => presetRow(p, "suggested"));
      const rest = presets.filter((p) => !sugIDs.includes(p.id)).map((p) => presetRow(p, "all"));
      return [...sug, ...rest, ...existing];
    }

    const scored = presets
      .map((p) => ({ row: presetRow(p, "all"), s: scorePreset(p, q) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.row);
    const needle = q.toLowerCase();
    const matchedExisting = existing.filter(
      (r) => r.label.toLowerCase().includes(needle) || r.key.toLowerCase().includes(needle),
    );
    return [...scored, ...matchedExisting];
  }, [graph, from, query, t]);

  useEffect(() => setActive(0), [query]);

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    inputRef.current?.focus();
    return () => prev?.focus?.();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          onClose();
          return;
        case "ArrowDown":
          e.preventDefault();
          setActive((i) => Math.min(rows.length - 1, i + 1));
          return;
        case "ArrowUp":
          e.preventDefault();
          setActive((i) => Math.max(0, i - 1));
          return;
        case "Home":
          e.preventDefault();
          setActive(0);
          return;
        case "End":
          e.preventDefault();
          setActive(rows.length - 1);
          return;
        case "Enter": {
          e.preventDefault();
          const hit = rows[active];
          if (hit) onPick(hit.choice);
          return;
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [rows, active, onPick, onClose]);

  useLayoutEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-qp-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active]);

  // Anchor near the drop point but keep the whole panel inside the canvas.
  const width = 340;
  const height = 420;
  const box = bounds ?? { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
  const left = Math.max(box.left + 8, Math.min(at.x, box.right - width - 8));
  const top = Math.max(box.top + 8, Math.min(at.y, box.bottom - height - 8));

  const heading = (g: Row["group"]) =>
    g === "suggested" ? t.t("Suggested") : g === "existing" ? t.t("Existing steps") : t.t("Everything");

  return (
    <div
      className="qp-backdrop"
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="qp" role="dialog" aria-modal="true" aria-label={t.t("Add a step")} style={{ left, top, width }}>
        <div className="qp__search">
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={
              from?.side === "output"
                ? t.t("What happens next?")
                : from?.side === "input"
                  ? t.t("What leads here?")
                  : t.t("Add a step…")
            }
            aria-label={t.t("Search steps")}
            spellCheck={false}
            autoComplete="off"
          />
        </div>
        <div className="qp__list" ref={listRef} role="listbox">
          {rows.length === 0 && <div className="qp__empty">{t.t("Nothing matches “{{q}}”", { q: query.trim() })}</div>}
          {rows.map((r, i) => (
            <Fragment key={r.key}>
              {(i === 0 || rows[i - 1]!.group !== r.group) && <div className="qp__group">{heading(r.group)}</div>}
              <button
                type="button"
                role="option"
                aria-selected={i === active}
                data-qp-index={i}
                className={"qp__row" + (i === active ? " is-active" : "")}
                onMouseEnter={() => setActive(i)}
                onClick={() => onPick(r.choice)}
              >
                <span className={`qp__dot qp__dot--${r.choice.kind === "preset" ? r.choice.preset.type : r.choice.node.type}`} />
                <span className="qp__text">
                  <span className="qp__label">{r.label}</span>
                  <span className="qp__hint">{r.hint}</span>
                </span>
              </button>
            </Fragment>
          ))}
        </div>
      </div>
    </div>
  );
}
