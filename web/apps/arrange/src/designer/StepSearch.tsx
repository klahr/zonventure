import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { blocksOf, textOf, type Graph, type Node } from "@zonventure/graph";
import { useT } from "../i18n.tsx";

/**
 * Find a step by what it says, opened with "/". Every step by default;
 * the chips narrow it to puzzles. Enter opens the step in the inspector.
 * Styled and driven like the quick-add palette (QuickPalette).
 */

type Scope = "puzzle" | "all";

interface Props {
  graph: Graph;
  /** Area to centre in — the canvas. Defaults to the window. */
  bounds?: DOMRect;
  onPick: (node: Node) => void;
  onClose: () => void;
}

interface Row {
  node: Node;
  label: string;
  hint: string;
}

/** What a step is found by: its title, its slug and its texts. */
function haystack(n: Node): string {
  return [n.title ?? "", n.slug, textOf(blocksOf(n))].join("\n").toLowerCase();
}

/** A short piece of the step's text around the first hit, for the hint. */
function snippet(n: Node, needle: string): string {
  const text = textOf(blocksOf(n)).replace(/\s+/g, " ").trim();
  if (!text) return "";
  const at = needle ? text.toLowerCase().indexOf(needle) : -1;
  const from = at > 30 ? at - 30 : 0;
  const piece = text.slice(from, from + 90);
  return (from > 0 ? "…" : "") + piece + (from + 90 < text.length ? "…" : "");
}

export function StepSearch({ graph, bounds, onPick, onClose }: Props) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>("all");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const rows = useMemo<Row[]>(() => {
    const needle = query.trim().toLowerCase();
    return graph.nodes
      .filter((n) => scope === "all" || n.type === "puzzle")
      .filter((n) => !needle || haystack(n).includes(needle))
      .map((n) => ({
        node: n,
        label: n.title || n.slug,
        hint: [scope === "all" ? n.type : "", n.slug, snippet(n, needle)].filter(Boolean).join(" · "),
      }));
  }, [graph, query, scope]);

  useEffect(() => setActive(0), [query, scope]);

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
        case "Tab":
          e.preventDefault();
          setScope((s) => (s === "all" ? "puzzle" : "all"));
          return;
        case "Enter": {
          e.preventDefault();
          const hit = rows[active];
          if (hit) onPick(hit.node);
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

  const width = 420;
  const box = bounds ?? { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
  const left = Math.max(box.left + 8, box.left + (box.right - box.left - width) / 2);
  const top = box.top + 48;

  return (
    <div
      className="qp-backdrop"
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="qp" role="dialog" aria-modal="true" aria-label={t.t("Find a step")} style={{ left, top, width }}>
        <div className="qp__search">
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={scope === "puzzle" ? t.t("Find a puzzle…") : t.t("Find a step…")}
            aria-label={t.t("Find a step")}
            spellCheck={false}
            autoComplete="off"
          />
        </div>
        <div className="qp__scope" role="radiogroup" aria-label={t.t("Search in")}>
          {(["all", "puzzle"] as const).map((s) => (
            <button
              key={s}
              type="button"
              role="radio"
              aria-checked={scope === s}
              onClick={() => {
                setScope(s);
                inputRef.current?.focus();
              }}
            >
              {s === "puzzle" ? t.t("Puzzles") : t.t("All steps")}
            </button>
          ))}
        </div>
        <div className="qp__list" ref={listRef} role="listbox">
          {rows.length === 0 && (
            <div className="qp__empty">
              {query.trim() ? t.t("Nothing matches “{{q}}”", { q: query.trim() }) : t.t("No puzzles yet")}
            </div>
          )}
          {rows.map((r, i) => (
            <button
              key={r.node.slug}
              type="button"
              role="option"
              aria-selected={i === active}
              data-qp-index={i}
              className={"qp__row" + (i === active ? " is-active" : "")}
              onMouseEnter={() => setActive(i)}
              onClick={() => onPick(r.node)}
            >
              <span className={`qp__dot qp__dot--${r.node.type}`} />
              <span className="qp__text">
                <span className="qp__label">{r.label}</span>
                <span className="qp__hint">{r.hint}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
