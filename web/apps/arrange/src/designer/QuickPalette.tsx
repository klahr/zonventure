import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  existingTargets,
  fillFields,
  portLabel,
  portsOf,
  presetsFor,
  scorePreset,
  suggestionsFor,
  targetOf,
  type DragFrom,
  type FillField,
  type Graph,
  type Node,
  type Preset,
  type StepFill,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";

/**
 * The quick-add palette, modelled on dazyflow's QuickDropPalette.
 *
 * It opens where a dragged wire was released on empty canvas (or on a
 * double-click, or with "a" or Ctrl+K after the selected step), lists what
 * can go there, and the pick is created at that spot already wired in.
 * Everything is reachable from the keyboard: type to filter, arrows to
 * move, Enter to pick, Escape to close.
 *
 * A click on a kind adds it at once. Enter on a kind goes on to ask for
 * its title, text and — for a typed puzzle — answer, one field at a time
 * (fillFields); Enter moves on, Ctrl+Enter adds the step with what is
 * filled in so far. Opened after a step with several outputs, it first
 * asks which one to follow.
 *
 * Behaviours carried over from dazyflow on purpose:
 *   - suggestions sit in their own group above the full list;
 *   - clicking the backdrop closes it;
 *   - the browser Back button closes it instead of leaving the designer,
 *     which is what a phone user or a mouse-back-button user expects of
 *     anything that looks like an overlay.
 */

export type PaletteChoice =
  | { kind: "preset"; preset: Preset; fill: StepFill; port?: string }
  | { kind: "existing"; node: Node; port?: string };

interface Props {
  graph: Graph;
  from: DragFrom | null;
  /** Ask which of `from`'s outputs to follow, when it has several. */
  choosePort?: boolean;
  /** Screen position to anchor near. */
  at: { x: number; y: number };
  /** Area to stay inside — the canvas, so the panel never covers the
   * inspector. Defaults to the window. */
  bounds?: DOMRect;
  onPick: (choice: PaletteChoice) => void;
  onClose: () => void;
}

type Phase = "port" | "type" | FillField;

interface Row {
  key: string;
  choice: PaletteChoice;
  label: string;
  hint: string;
  group: "suggested" | "all" | "existing";
}

let pendingHistoryPop: ReturnType<typeof setTimeout> | null = null;

export function QuickPalette({ graph, from, choosePort = false, at, bounds, onPick, onClose }: Props) {
  const t = useT();
  const ports = from?.side === "output" ? portsOf(from.node) : [];
  const [port, setPort] = useState(from?.side === "output" ? from.port : undefined);
  const [phase, setPhase] = useState<Phase>(choosePort && ports.length > 1 ? "port" : "type");
  const [query, setQuery] = useState("");
  // Typing in the port phase narrows the outputs by name.
  const [portQuery, setPortQuery] = useState("");
  const portNeedle = portQuery.trim().toLowerCase();
  const shownPorts = portNeedle
    ? ports.filter((p) => (from?.side === "output" ? portLabel(from.node, p, graph.roles) : p).toLowerCase().includes(portNeedle))
    : ports;
  const [active, setActive] = useState(() => (phase === "port" ? Math.max(0, ports.indexOf(port ?? "")) : 0));
  const [preset, setPreset] = useState<Preset | null>(null);
  const [fill, setFill] = useState<StepFill>({});
  const inputRef = useRef<HTMLInputElement & HTMLTextAreaElement>(null);
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

  // The drag the kinds are offered for: from the output chosen so far.
  const drag = useMemo<DragFrom | null>(
    () => (from?.side === "output" && port ? { ...from, port } : from),
    [from, port],
  );

  const rows = useMemo<Row[]>(() => {
    const presets = presetsFor(drag);
    const q = query.trim();

    const presetRow = (p: Preset, group: Row["group"]): Row => ({
      key: "p:" + p.id,
      choice: { kind: "preset", preset: p, fill: {} },
      label: t.t(p.label),
      hint: t.t(p.hint),
      group,
    });
    const existing = existingTargets(graph, drag).map<Row>((n) => ({
      key: "n:" + n.slug,
      choice: { kind: "existing", node: n },
      label: n.title || n.slug,
      hint: t.t("Connect to this existing step") + ` · ${n.type}`,
      group: "existing",
    }));

    if (!q) {
      const sugIDs = suggestionsFor(drag);
      const sug = sugIDs
        .map((id) => presets.find((p) => p.id === id))
        .filter((p): p is Preset => !!p)
        .map((p) => presetRow(p, "suggested"));
      const rest = presets.filter((p) => !sugIDs.includes(p.id)).map((p) => presetRow(p, "all"));
      return [...sug, ...rest, ...existing];
    }

    const scored = presets
      .map((p) => ({ row: presetRow(p, "all"), s: scorePreset(p, q, (s) => t.t(s)) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.row);
    const needle = q.toLowerCase();
    const matchedExisting = existing.filter(
      (r) => r.label.toLowerCase().includes(needle) || r.key.toLowerCase().includes(needle),
    );
    return [...scored, ...matchedExisting];
  }, [graph, drag, query, t]);

  const count = phase === "port" ? shownPorts.length : phase === "type" ? rows.length : 0;

  useEffect(() => {
    if (phase === "type") setActive(0);
  }, [query, phase]);
  useEffect(() => {
    if (phase === "port" && portNeedle) setActive(0);
  }, [portNeedle, phase]);

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    inputRef.current?.focus();
    return () => prev?.focus?.();
  }, []);
  useEffect(() => inputRef.current?.focus(), [phase]);

  // Picking, and the fields after it.
  const add = (p: Preset, f: StepFill) => onPick({ kind: "preset", preset: p, fill: f, ...(port ? { port } : {}) });
  const choose = (row: Row, andFill: boolean) => {
    if (row.choice.kind === "existing") {
      onPick({ ...row.choice, ...(port ? { port } : {}) });
      return;
    }
    const p = row.choice.preset;
    const first = fillFields(p)[0];
    if (!andFill || !first) {
      add(p, {});
      return;
    }
    setPreset(p);
    setQuery("");
    setPhase(first);
  };
  const next = () => {
    if (!preset) return;
    const order = fillFields(preset);
    const after = order[order.indexOf(phase as FillField) + 1];
    if (after) setPhase(after);
    else add(preset, fill);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          onClose();
          return;
        case "ArrowDown":
        case "ArrowUp":
        case "Home":
        case "End":
          if (!count) return;
          e.preventDefault();
          setActive((i) =>
            e.key === "ArrowDown" ? Math.min(count - 1, i + 1) : e.key === "ArrowUp" ? Math.max(0, i - 1) : e.key === "Home" ? 0 : count - 1,
          );
          return;
        case "Enter": {
          if (phase === "text" && e.shiftKey) return;
          e.preventDefault();
          const now = e.ctrlKey || e.metaKey;
          if (phase === "port") {
            const p = shownPorts[active];
            if (p) {
              setPort(p);
              setPhase("type");
            }
          } else if (phase === "type") {
            const hit = rows[active];
            if (hit) choose(hit, !now);
          } else if (preset) {
            if (now) add(preset, fill);
            else next();
          }
          return;
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

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

  const shownPort = phase === "port" ? shownPorts[active] : port;
  const where =
    from?.side === "output" && choosePort
      ? ports.length > 1 && shownPort
        ? t.t("After {{step}} · {{port}}", { step: from.node.title || from.node.slug, port: portLabel(from.node, shownPort, graph.roles) })
        : t.t("After {{step}}", { step: from.node.title || from.node.slug })
      : "";
  const done = [preset && t.t(preset.label), fill.title?.trim()].filter(Boolean).join(" · ");
  const fieldLabel: Record<FillField, string> = {
    title: t.t("Title"),
    text: t.t("What the team sees (Shift+Enter for a new line)"),
    answer: t.t("Accepted answer"),
  };
  const setField = (f: FillField) => (v: string) => setFill((x) => ({ ...x, [f]: v }));

  return (
    <div
      className="qp-backdrop"
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="qp" role="dialog" aria-modal="true" aria-label={t.t("Add a step")} style={{ left, top, width }}>
        {(where || done) && (
          <div className="qa__where">
            {where}
            {done && <span className="qa__done">{where ? " — " : ""}{done}</span>}
          </div>
        )}
        {phase !== "type" && (
          <label className="qa__label" htmlFor="qp-input">
            {phase === "port" ? t.t("Which output?") : fieldLabel[phase]}
          </label>
        )}
        <div className="qp__search">
          {phase === "text" ? (
            <textarea id="qp-input" ref={inputRef} rows={4} value={fill.text ?? ""} onChange={(e) => setField("text")(e.target.value)} />
          ) : phase === "port" ? (
            <input
              id="qp-input"
              ref={inputRef}
              value={portQuery}
              onChange={(e) => setPortQuery(e.target.value)}
              placeholder={from?.side === "output" && shownPort ? portLabel(from.node, shownPort, graph.roles) : ""}
              spellCheck={false}
              autoComplete="off"
            />
          ) : phase === "type" ? (
            <input
              id="qp-input"
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
          ) : (
            <input
              id="qp-input"
              ref={inputRef}
              value={fill[phase] ?? ""}
              placeholder={phase === "title" && preset ? t.t(preset.label) : ""}
              onChange={(e) => setField(phase)(e.target.value)}
              autoComplete="off"
            />
          )}
        </div>
        {phase === "port" && (
          <div className="qp__list" ref={listRef} role="listbox">
            {shownPorts.map((p, i) => (
              <button
                key={p}
                type="button"
                role="option"
                aria-selected={i === active}
                data-qp-index={i}
                className={"qp__row" + (i === active ? " is-active" : "")}
                onMouseEnter={() => setActive(i)}
                onClick={() => {
                  setPort(p);
                  setPhase("type");
                }}
              >
                <span className="qp__text">
                  <span className="qp__label">{from?.side === "output" ? portLabel(from.node, p, graph.roles) : p}</span>
                  <span className="qp__hint">
                    {from && targetOf(graph, from.node.slug, p) ? t.t("Already leads somewhere; the new step goes in between") : t.t("Not connected yet")}
                  </span>
                </span>
              </button>
            ))}
          </div>
        )}
        {phase === "type" && (
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
                  onClick={() => choose(r, false)}
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
        )}
        <div className="qa__keys">
          {phase === "type"
            ? t.t("Click to add · Enter to fill in details · Ctrl+Enter to add now")
            : phase === "port"
              ? t.t("Enter to continue · Esc to cancel")
              : t.t("Enter to continue · Ctrl+Enter to add now · Esc to cancel")}
        </div>
      </div>
    </div>
  );
}
