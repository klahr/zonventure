import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  BackgroundVariant,
  ControlButton,
  Controls,
  MiniMap,
  ReactFlow,
  applyEdgeChanges,
  applyNodeChanges,
  useNodesInitialized,
  useReactFlow,
  type Connection,
  type Edge as RFEdge,
  type EdgeChange,
  type FinalConnectionState,
  type NodeChange,
  type OnConnectStartParams,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  addNode,
  insertAfter,
  insertionPoint,
  connect,
  connectionError,
  fillStep,
  firstPort,
  moveNode,
  nodeBySlug,
  nodeFromPreset,
  reconcileByID,
  removeNode,
  disconnect,
  freshSlug,
  startZone,
  sameData,
  samePosition,
  upgradeNode,
  withFixed,
  type DragFrom,
  type Graph,
  type Issue,
  type Size,
  type Frame,
} from "@zonventure/graph";
import { Frame as FrameIcon } from "lucide-react";
import { routesIn, useT } from "../i18n.tsx";
import { CanvasContextMenu, type ContextMenuItem } from "./dz/CanvasContextMenu.tsx";
import { ICON } from "./dz/ui.tsx";
import { NodeCard, TYPE_LABEL, type CardNode, type Mark } from "./NodeCard.tsx";
import { LiveLayer, type CanvasLive } from "./Live.tsx";
import type { CardTalk } from "./Collab.tsx";
import { FrameCard, FRAME_COLOR_DEFAULT, type FrameNode } from "./FrameCard.tsx";
import { QuickPalette, type PaletteChoice } from "./QuickPalette.tsx";

// Copied steps, shared across designer tabs the way dazyflow's clipboard
// is: a JSON payload under one key.
const CLIPBOARD = "__zonventure_clipboard";

type GraphNode = Graph["nodes"][number];

/** A copy of a step with a fresh slug, offset so it does not hide the original. */
function copyNode(g: Graph, n: GraphNode, dx = 40, dy = 40): GraphNode {
  return { ...structuredClone(n), slug: freshSlug(g, n.type), x: (n.x ?? 0) + dx, y: (n.y ?? 0) + dy };
}

const nodeTypes = { zv: NodeCard, frame: FrameCard };

/** Frames live beside the steps in React Flow; their ids never clash with a slug. */
const FRAME_PREFIX = "frame:";
const isFrameID = (id: string) => id.startsWith(FRAME_PREFIX);
type AnyNode = CardNode | FrameNode;

interface Props {
  graph: Graph;
  issues: Issue[];
  selected: string | null;
  onChange: (g: Graph) => void;
  onSelect: (slug: string | null) => void;
  /** Bumped by the parent to ask the canvas to open the palette at its centre. */
  paletteRequest: number;
  /** Bumped by the parent to centre the canvas on the selected node. */
  focusRequest: number;
  /** Bumped when the keyboard moved the selection: pan to it if it is off screen. */
  revealRequest?: number;
  /** Where the simulated team is, so the canvas can show it. */
  playing?: string | null;
  /** Steps someone else just changed, to light up in their colour. */
  changed?: ReadonlyMap<string, string>;
  /** Who else has each step open, or is working on it. */
  marks?: ReadonlyMap<string, Mark[]>;
  /** What someone else just did, to animate. */
  fx?: LiveFx;
  /** Others' cursors and views, and this page's for them. */
  live?: CanvasLive;
  /** The work on each step: status, owner, conversations, unseen. */
  talk?: ReadonlyMap<string, CardTalk>;
}

/** Someone else's arrivals, removals and new wires, animated for a moment. */
export interface LiveFx {
  arrived: ReadonlySet<string>;
  /** Steps just removed, drawn fading out where they were. */
  leaving: readonly GraphNode[];
  /** Wires just drawn, by React Flow id ("from:port"). */
  wired: ReadonlySet<string>;
}

const GHOST_PREFIX = "ghost:";

interface PendingDrag {
  nodeId: string;
  handleId: string | null;
  handleType: "source" | "target";
}

/**
 * The rendered size of every card, for Tidy. Read from the DOM so the
 * toolbar, outside the canvas's React Flow context, can use it too;
 * offset sizes ignore the zoom transform. A card with a map or a picture
 * is much taller than the default tidy assumes.
 */
export function cardSizes(): Map<string, Size> {
  const sizes = new Map<string, Size>();
  for (const el of document.querySelectorAll<HTMLElement>(".dz-flow .react-flow__node[data-id]")) {
    sizes.set(el.dataset.id!, { w: el.offsetWidth, h: el.offsetHeight });
  }
  return sizes;
}

/** Below this zoom a card's text is too small to read. */
const READABLE_ZOOM = 0.6;
/** The zoom a large adventure opens at, around its start. */
const START_ZOOM = 0.75;

/**
 * The Blueprint canvas.
 *
 * The graph document is the single source of truth. React Flow keeps its
 * own node array for smooth dragging and selection; it is reconciled from
 * the document whenever the document changes — keeping unchanged objects
 * by reference (reconcileByID, from dazyflow) so an undo re-renders only
 * the cards that actually changed — and positions flow back into the
 * document when a drag ends.
 *
 * The signature interaction, taken from dazyflow: drag a wire out of a
 * port and release it on empty canvas, and the quick palette opens there.
 * The pick is created at that spot and wired in. Releasing on a handle
 * that refuses the wire shows why instead.
 */
export function Canvas({ graph, issues, selected, onChange, onSelect, paletteRequest, focusRequest, revealRequest = 0, playing = null, changed, marks, fx, live, talk }: Props) {
  const rf = useReactFlow();
  const t = useT();
  const ariaLabels = useMemo(
    () => ({
      "controls.ariaLabel": t.t("Canvas controls"),
      "controls.zoomIn.ariaLabel": t.t("Zoom in"),
      "controls.zoomOut.ariaLabel": t.t("Zoom out"),
      "controls.fitView.ariaLabel": t.t("Fit to screen"),
      "minimap.ariaLabel": t.t("Overview map"),
    }),
    [t],
  );
  const [menu, setMenu] = useState<{ x: number; y: number; items: ContextMenuItem[] } | null>(null);
  const [nodes, setNodes] = useState<CardNode[]>([]);
  const [frameNodes, setFrameNodes] = useState<FrameNode[]>([]);
  // The steps a frame carries along while it is dragged, and where each
  // started; taken when the drag begins.
  const frameDrag = useRef<{ id: string; x: number; y: number; members: { id: string; x: number; y: number }[] } | null>(null);
  const [edges, setEdges] = useState<RFEdge[]>([]);
  // `insert`: opened from the toolbar, with no wire and no spot picked,
  // so the new step is attached where the flow is being built.
  const [palette, setPalette] = useState<{ from: DragFrom | null; screen: { x: number; y: number }; insert?: boolean } | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const pending = useRef<PendingDrag | null>(null);
  const connected = useRef(false);
  const wrapper = useRef<HTMLDivElement>(null);

  // The first view: the whole adventure when it fits at a zoom its text
  // can be read at, otherwise a readable zoom with the start(s) at the
  // left edge, the flow running off to the right. Fit View still shows
  // everything on demand.
  const nodesReady = useNodesInitialized();
  const framed = useRef(false);
  useEffect(() => {
    if (!nodesReady || framed.current) return;
    framed.current = true;
    void (async () => {
      await rf.fitView({ padding: 0.2, maxZoom: 1.1 });
      if (rf.getZoom() >= READABLE_ZOOM) return;
      const cards = rf.getNodes().filter((n) => !isFrameID(n.id));
      const starts = cards.filter((n) => (n as CardNode).data.node.type === "start");
      const anchor = starts.length ? starts : cards.slice(0, 1);
      const r = wrapper.current?.getBoundingClientRect();
      if (!anchor.length || !r) return;
      const x0 = Math.min(...anchor.map((n) => n.position.x));
      const ys = anchor.flatMap((n) => [n.position.y, n.position.y + (n.measured?.height ?? 110)]);
      const cy = (Math.min(...ys) + Math.max(...ys)) / 2;
      await rf.setViewport({ x: r.width * 0.06 - x0 * START_ZOOM, y: r.height / 2 - cy * START_ZOOM, zoom: START_ZOOM });
    })();
  }, [nodesReady, rf]);

  const issuesBySlug = useMemo(() => {
    const m = new Map<string, Issue[]>();
    for (const i of issues) {
      if (!i.node) continue;
      const list = m.get(i.node);
      if (list) list.push(i);
      else m.set(i.node, [i]);
    }
    return m;
  }, [issues]);

  const sourceTitle = (n: Graph["nodes"][number]): string | undefined => {
    if (n.effect?.kind === "grant_item") return graph.items?.find((i) => i.slug === n.effect!.item)?.name || undefined;
    const from = n.switch?.from;
    if (!from) return undefined;
    const src = graph.nodes.find((x) => x.slug === from);
    return src?.title || from;
  };

  // Document → React Flow nodes, reusing unchanged objects so memoised
  // cards do not re-render.
  useEffect(() => {
    setNodes((prev) =>
      reconcileByID(prev, graph.nodes, {
        idOfExisting: (n) => n.id,
        idOfTarget: (n) => n.slug,
        isUnchanged: (existing, n) =>
          existing.data.node === n &&
          sameData(existing.data.issues, issuesBySlug.get(n.slug) ?? []) &&
          existing.selected === (n.slug === selected) &&
          existing.data.playing === (n.slug === playing) &&
          existing.data.changed === changed?.get(n.slug) &&
          existing.data.marks === marks?.get(n.slug) &&
          existing.data.talk === talk?.get(n.slug) &&
          !!existing.data.arrived === !!fx?.arrived.has(n.slug) &&
          existing.data.sourceTitle === sourceTitle(n) &&
          existing.data.zone === startZone(graph, n) &&
          samePosition(existing.position, { x: n.x ?? 0, y: n.y ?? 0 }) &&
          existing.ariaLabel === t.t("{{kind}}: {{title}}", { kind: t.t(TYPE_LABEL[n.type]), title: n.title || n.slug }),
        build: (n, previous) => ({
          id: n.slug,
          type: "zv" as const,
          position: { x: n.x ?? 0, y: n.y ?? 0 },
          data: {
            node: n,
            issues: issuesBySlug.get(n.slug) ?? [],
            playing: n.slug === playing,
            ...(changed?.has(n.slug) ? { changed: changed.get(n.slug)! } : {}),
            ...(marks?.has(n.slug) ? { marks: marks.get(n.slug)! } : {}),
            ...(talk?.has(n.slug) ? { talk: talk.get(n.slug)! } : {}),
            ...(fx?.arrived.has(n.slug) ? { arrived: true } : {}),
            ...(sourceTitle(n) ? { sourceTitle: sourceTitle(n)! } : {}), ...(startZone(graph, n) ? { zone: startZone(graph, n)! } : {}) },
          selected: n.slug === selected,
          ariaLabel: t.t("{{kind}}: {{title}}", { kind: t.t(TYPE_LABEL[n.type]), title: n.title || n.slug }),
          // Keep React Flow's measurement so handles do not jump.
          ...(previous?.measured ? { measured: previous.measured } : {}),
          // Any start may go while another remains.
          deletable: n.type !== "start" || graph.nodes.filter((x) => x.type === "start").length > 1,
        }),
      }).items,
    );
  }, [graph, issuesBySlug, selected, playing, changed, marks, fx, talk, t]);

  // Steps someone else removed linger a moment, fading, where they were.
  const ghosts = useMemo<CardNode[]>(
    () =>
      (fx?.leaving ?? []).map((n) => ({
        id: GHOST_PREFIX + n.slug,
        type: "zv" as const,
        position: { x: n.x ?? 0, y: n.y ?? 0 },
        data: { node: n, issues: [], leaving: true },
        selectable: false,
        draggable: false,
        connectable: false,
        deletable: false,
        focusable: false,
        ariaRole: "presentation",
      })),
    [fx?.leaving],
  );

  // Frames: the document's boxes, behind every card.
  const graphRefForFrames = useRef(graph);
  graphRefForFrames.current = graph;
  const setFrame = useCallback(
    (id: string, patch: Partial<Frame> | null) => {
      const g = graphRefForFrames.current;
      const frames = (g.frames ?? []).flatMap((f) => (f.id !== id ? [f] : patch ? [{ ...f, ...patch }] : []));
      onChange({ ...g, frames });
    },
    [onChange],
  );
  useEffect(() => {
    setFrameNodes((prev) =>
      (graph.frames ?? []).map((f) => {
        const id = FRAME_PREFIX + f.id;
        const old = prev.find((p) => p.id === id);
        return {
          id,
          type: "frame" as const,
          position: { x: f.x, y: f.y },
          width: f.width,
          height: f.height,
          zIndex: -1,
          connectable: false,
          selected: old?.selected ?? false,
          data: {
            ...(f.title ? { title: f.title } : {}),
            color: f.color || FRAME_COLOR_DEFAULT,
            onTitleChange: (title: string) => setFrame(f.id, { title }),
            onColorChange: (color: string) => setFrame(f.id, { color }),
            onRequestDelete: () => setFrame(f.id, null),
          },
        };
      }),
    );
  }, [graph.frames, setFrame]);

  /** Wrap the selected steps in a frame, or drop an empty one in view. */
  const addFrame = () => {
    const picked = rf.getNodes().filter((n) => n.selected && !isFrameID(n.id));
    let box: { x: number; y: number; width: number; height: number };
    if (picked.length) {
      const pad = 40;
      const xs = picked.flatMap((n) => [n.position.x, n.position.x + (n.measured?.width ?? 240)]);
      const ys = picked.flatMap((n) => [n.position.y, n.position.y + (n.measured?.height ?? 110)]);
      box = { x: Math.min(...xs) - pad, y: Math.min(...ys) - pad - 24, width: Math.max(...xs) - Math.min(...xs) + 2 * pad, height: Math.max(...ys) - Math.min(...ys) + 2 * pad + 24 };
    } else {
      const r = wrapper.current?.getBoundingClientRect();
      const c = rf.screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 800) / 2, y: (r?.top ?? 0) + (r?.height ?? 600) / 2 });
      box = { x: c.x - 240, y: c.y - 160, width: 480, height: 320 };
    }
    const used = new Set((graph.frames ?? []).map((f) => f.id));
    let n = (graph.frames?.length ?? 0) + 1;
    while (used.has(`f${n}`)) n++;
    onChange({ ...graph, frames: [...(graph.frames ?? []), { id: `f${n}`, title: "", ...box }] });
  };

  useEffect(() => {
    setEdges((prev) => {
      const selectedIDs = new Set(prev.filter((e) => e.selected).map((e) => e.id));
      const stepName = (slug: string) => graph.nodes.find((x) => x.slug === slug)?.title || slug;
      return graph.edges.map((e) => {
        const id = `${e.from}:${e.from_port}`;
        const bad = issues.some((i) => i.node === e.from && i.port === e.from_port);
        return {
          id,
          source: e.from,
          sourceHandle: e.from_port,
          target: e.to,
          targetHandle: "in",
          ariaLabel: t.t("From {{from}} to {{to}}", { from: stepName(e.from), to: stepName(e.to) }),
          selected: selectedIDs.has(id),
          className: (bad ? "edge--bad" : "") + (fx?.wired.has(id) ? " edge--new" : ""),
        };
      });
    });
  }, [graph.edges, graph.nodes, issues, fx?.wired, t]);

  const onNodesChange = useCallback(
    (all: NodeChange<AnyNode>[]) => {
      const frameChanges = all.filter((c) => "id" in c && isFrameID(c.id)) as NodeChange<FrameNode>[];
      const changes = all.filter((c) => !("id" in c && (isFrameID(c.id) || c.id.startsWith(GHOST_PREFIX)))) as NodeChange<CardNode>[];
      let next = graph;
      if (frameChanges.length) {
        setFrameNodes((fs) => applyNodeChanges(frameChanges, fs));
        for (const c of frameChanges) {
          if (!("id" in c)) continue;
          const fid = c.id.slice(FRAME_PREFIX.length);
          const f = (next.frames ?? []).find((x) => x.id === fid);
          if (!f) continue;
          if (c.type === "position" && c.position) {
            // A frame carries the steps whose centre is inside it.
            if (c.dragging && !frameDrag.current) {
              const inside = nodes.filter((n) => {
                const cx = n.position.x + (n.measured?.width ?? 240) / 2;
                const cy = n.position.y + (n.measured?.height ?? 110) / 2;
                return cx >= f.x && cx <= f.x + f.width && cy >= f.y && cy <= f.y + f.height;
              });
              frameDrag.current = { id: c.id, x: f.x, y: f.y, members: inside.map((n) => ({ id: n.id, x: n.position.x, y: n.position.y })) };
            }
            const drag = frameDrag.current?.id === c.id ? frameDrag.current : null;
            const dx = c.position.x - (drag?.x ?? f.x);
            const dy = c.position.y - (drag?.y ?? f.y);
            if (drag) {
              const at = new Map(drag.members.map((m) => [m.id, m]));
              setNodes((ns) => ns.map((n) => (at.has(n.id) ? { ...n, position: { x: at.get(n.id)!.x + dx, y: at.get(n.id)!.y + dy } } : n)));
            }
            if (c.dragging === false || c.dragging === undefined) {
              next = { ...next, frames: (next.frames ?? []).map((x) => (x.id === fid ? { ...x, x: c.position!.x, y: c.position!.y } : x)) };
              if (drag) {
                for (const m of drag.members) next = moveNode(next, m.id, m.x + dx, m.y + dy);
                frameDrag.current = null;
              }
            }
          }
          if (c.type === "dimensions" && c.dimensions && c.resizing === false) {
            next = { ...next, frames: (next.frames ?? []).map((x) => (x.id === fid ? { ...x, width: c.dimensions!.width, height: c.dimensions!.height } : x)) };
          }
        }
      }
      setNodes((ns) => applyNodeChanges(changes, ns));
      for (const c of changes) {
        // Commit a position only when the drag ends, so a drag is one
        // document change (and one undo step) rather than hundreds.
        if (c.type === "position" && c.dragging === false && c.position) {
          next = moveNode(next, c.id, c.position.x, c.position.y);
        }
        if (c.type === "select" && c.selected) onSelect(c.id);
      }
      if (next !== graph) onChange(next);
    },
    [graph, nodes, onChange, onSelect],
  );

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    setEdges((es) => applyEdgeChanges(changes, es));
  }, []);

  const onNodesDelete = useCallback(
    (deleted: AnyNode[]) => {
      let next = graph;
      const gone = new Set(deleted.filter((n) => isFrameID(n.id)).map((n) => n.id.slice(FRAME_PREFIX.length)));
      if (gone.size) next = { ...next, frames: (next.frames ?? []).filter((f) => !gone.has(f.id)) };
      for (const n of deleted) if (!isFrameID(n.id)) next = removeNode(next, n.id);
      onChange(next);
      onSelect(null);
    },
    [graph, onChange, onSelect],
  );

  const onEdgesDelete = useCallback(
    (deleted: RFEdge[]) => {
      let next = graph;
      for (const e of deleted) if (e.sourceHandle) next = disconnect(next, e.source, e.sourceHandle);
      onChange(next);
    },
    [graph, onChange],
  );

  const isValidConnection = useCallback(
    (c: Connection | RFEdge) =>
      !!c.sourceHandle && connectionError(graph, c.source, c.sourceHandle, c.target) === null,
    [graph],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      connected.current = true;
      if (c.sourceHandle) onChange(connect(graph, c.source, c.sourceHandle, c.target));
    },
    [graph, onChange],
  );

  const onConnectStart = useCallback((_: unknown, p: OnConnectStartParams) => {
    connected.current = false;
    pending.current = p.nodeId
      ? { nodeId: p.nodeId, handleId: p.handleId, handleType: p.handleType ?? "source" }
      : null;
  }, []);

  const onConnectEnd = useCallback(
    (e: MouseEvent | TouchEvent, conn: FinalConnectionState) => {
      const start = pending.current;
      pending.current = null;
      if (connected.current || !start) return;

      // Dropped on a handle that refused the wire: say why, and do not
      // also open the palette — the author aimed at something.
      if (conn.toHandle && conn.isValid === false) {
        const fromOut = start.handleType === "source";
        const from = fromOut ? start.nodeId : conn.toHandle.nodeId;
        const port = fromOut ? start.handleId ?? "" : conn.toHandle.id ?? "";
        const to = fromOut ? conn.toHandle.nodeId : start.nodeId;
        setHint(connectionError(graph, from, port, to) ?? "That connection is not allowed.");
        return;
      }

      const node = nodeBySlug(graph, start.nodeId);
      if (!node) return;
      const pt =
        "clientX" in e
          ? { x: e.clientX, y: e.clientY }
          : { x: e.changedTouches[0]?.clientX ?? 0, y: e.changedTouches[0]?.clientY ?? 0 };

      // An output dropped on another step's card, not on its input dot:
      // the author aimed at that step, so wire it there.
      if (start.handleType === "source") {
        const to = document.elementFromPoint(pt.x, pt.y)?.closest<HTMLElement>(".react-flow__node")?.dataset.id;
        if (to && to !== start.nodeId && nodeBySlug(graph, to)) {
          const port = start.handleId ?? "";
          const err = connectionError(graph, start.nodeId, port, to);
          if (err === null) onChange(connect(graph, start.nodeId, port, to));
          else setHint(err);
          return;
        }
      }

      const from: DragFrom =
        start.handleType === "source"
          ? { side: "output", node, port: start.handleId ?? "" }
          : { side: "input", node };
      setPalette({ from, screen: pt });
    },
    [graph, onChange],
  );

  const pick = useCallback(
    (choice: PaletteChoice) => {
      if (!palette) return;
      const { from, screen, insert } = palette;
      setPalette(null);

      const port = choice.port ?? (from?.side === "output" ? from.port : undefined);
      if (choice.kind === "existing") {
        if (from?.side === "output" && port) onChange(connect(graph, from.node.slug, port, choice.node.slug));
        onSelect(choice.node.slug);
        return;
      }
      const build = (at: { x: number; y: number }) => fillStep(routesIn(nodeFromPreset(graph, choice.preset, at), graph.language), choice.preset, choice.fill);

      // From the toolbar, "a" or Ctrl+K: after the selected step, or
      // before the End, wired in and with room made for it — not dropped
      // loose on top of whatever is under the middle of the screen.
      if (insert && from?.side === "output" && port) {
        const next = insertAfter(graph, { from: from.node.slug, port }, build({ x: 0, y: 0 }));
        const added = next.nodes[next.nodes.length - 1]!;
        onChange(next);
        onSelect(added.slug);
        void rf.setCenter((added.x ?? 0) + 120, (added.y ?? 0) + 60, { zoom: rf.getZoom(), duration: 300 });
        return;
      }

      // Place the card so its input handle lands under the cursor, as if
      // the wire had been dropped straight onto it.
      const flow = rf.screenToFlowPosition(screen);
      const at = from?.side === "input" ? { x: flow.x - 240, y: flow.y - 40 } : { x: flow.x, y: flow.y - 40 };
      const node = build(at);
      let next = addNode(graph, node);
      if (from?.side === "output") next = connect(next, from.node.slug, from.port, node.slug);
      if (from?.side === "input") {
        const p = firstPort(node);
        if (p) next = connect(next, node.slug, p, from.node.slug);
      }
      onChange(next);
      onSelect(node.slug);
      // Selecting it opens the inspector over the canvas's right side (or
      // narrows the canvas): a card dropped there would land behind it, so
      // pan just enough to show it — and not at all when it is in view.
      requestAnimationFrame(() => {
        const el = wrapper.current;
        const editor = el?.closest<HTMLElement>(".editor");
        if (!el || !editor) return;
        const r = el.getBoundingClientRect();
        const right = Math.min(r.right, editor.getBoundingClientRect().right - (parseFloat(getComputedStyle(editor).getPropertyValue("--dz-inspector-w")) || 0));
        if (right - r.left < 240 * rf.getZoom()) return;
        const tl = rf.flowToScreenPosition(at);
        const br = rf.flowToScreenPosition({ x: at.x + 240, y: at.y + 120 });
        const dx = tl.x < r.left || br.x > right ? (r.left + right) / 2 - (tl.x + br.x) / 2 : 0;
        const dy = tl.y < r.top || br.y > r.bottom ? (r.top + r.bottom) / 2 - (tl.y + br.y) / 2 : 0;
        if (!dx && !dy) return;
        const vp = rf.getViewport();
        void rf.setViewport({ x: vp.x + dx, y: vp.y + dy, zoom: vp.zoom }, { duration: 300 });
      });
    },
    [palette, graph, onChange, onSelect, rf],
  );

  // --- dazyflow's context menus ------------------------------------------------

  const duplicate = useCallback(
    (slug: string) => {
      const n = nodeBySlug(graph, slug);
      if (!n) return;
      const c = copyNode(graph, n);
      onChange(addNode(graph, c));
      onSelect(c.slug);
    },
    [graph, onChange, onSelect],
  );

  const onNodeContextMenu = useCallback(
    (e: React.MouseEvent, any: AnyNode) => {
      e.preventDefault();
      // A frame has its own tools; the step menu does not apply to it.
      if (any.type !== "zv" || any.id.startsWith(GHOST_PREFIX)) return;
      const n = any as CardNode;
      onSelect(n.id);
      const isStart = n.data.node.type === "start" && graph.nodes.filter((x) => x.type === "start").length === 1;
      setMenu({
        x: e.clientX,
        y: e.clientY,
        items: [
          { label: t.t("Duplicate"), onClick: () => duplicate(n.id), shortcut: "Ctrl+D" },
          { label: t.t("Copy"), onClick: () => copySteps([n.id]), shortcut: "Ctrl+C" },
          { separator: true },
          {
            label: t.t("Delete"),
            danger: true,
            disabled: isStart,
            shortcut: "Del",
            onClick: () => {
              onChange(removeNode(graph, n.id));
              onSelect(null);
            },
          },
        ],
      });
    },
    // copySteps is stable enough: it reads graph through its own closure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [graph, duplicate, onChange, onSelect, t],
  );

  const onEdgeContextMenu = useCallback(
    (e: React.MouseEvent, edge: RFEdge) => {
      e.preventDefault();
      setMenu({
        x: e.clientX,
        y: e.clientY,
        items: [
          { header: t.t("Connection") },
          {
            label: t.t("Delete connection"),
            danger: true,
            shortcut: "Del",
            onClick: () => edge.sourceHandle && onChange(disconnect(graph, edge.source, edge.sourceHandle)),
          },
        ],
      });
    },
    [graph, onChange, t],
  );

  // Right-clicking empty canvas opens the step palette at the cursor, as
  // in dazyflow — there is no separate pane menu.
  const onPaneContextMenu = useCallback((e: MouseEvent | React.MouseEvent) => {
    e.preventDefault();
    setPalette({ from: null, screen: { x: e.clientX, y: e.clientY } });
  }, []);

  // --- copy and paste ------------------------------------------------------------

  const copySteps = (slugs: string[]) => {
    const picked = graph.nodes.filter((n) => slugs.includes(n.slug));
    if (!picked.length) return;
    try {
      localStorage.setItem(CLIPBOARD, JSON.stringify(picked));
    } catch {
      /* private mode: copy within this tab only is not worth a fallback */
    }
  };
  const graphRef = useRef(graph);
  graphRef.current = graph;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      if ((e.target as HTMLElement)?.closest?.("input, textarea, select, [contenteditable]")) return;
      const k = e.key.toLowerCase();
      const g = graphRef.current;
      const sel = rf.getNodes().filter((n) => n.selected).map((n) => n.id);
      if (k === "c" && sel.length) {
        copySteps(sel);
      } else if (k === "d" && sel.length === 1) {
        e.preventDefault();
        duplicate(sel[0]!);
      } else if (k === "v") {
        let raw: string | null = null;
        try {
          raw = localStorage.getItem(CLIPBOARD);
        } catch {
          raw = null;
        }
        let copied: GraphNode[];
        try {
          copied = raw ? (JSON.parse(raw) as GraphNode[]).map((n) => withFixed(upgradeNode(n))) : [];
        } catch {
          copied = [];
        }
        if (!copied.length) return;
        e.preventDefault();
        let next = g;
        let last = "";
        for (const n of copied) {
          const c = copyNode(next, n, 60, 60);
          next = addNode(next, c);
          last = c.slug;
        }
        onChange(next);
        if (last) onSelect(last);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [duplicate, onChange, onSelect, rf]);

  // Double-clicking empty canvas opens the palette there, unconnected.
  const onDoubleClick = useCallback((e: React.MouseEvent) => {
    if ((e.target as HTMLElement).classList.contains("react-flow__pane")) {
      setPalette({ from: null, screen: { x: e.clientX, y: e.clientY } });
    }
  }, []);

  // Ctrl+K from the parent: open at the middle of the canvas.
  useEffect(() => {
    if (!paletteRequest || !wrapper.current) return;
    const r = wrapper.current.getBoundingClientRect();
    const point = insertionPoint(graph, selected);
    const node = point ? nodeBySlug(graph, point.from) : undefined;
    setPalette({
      from: node && point ? { side: "output", node, port: point.port } : null,
      screen: { x: r.left + r.width / 2 - 170, y: r.top + r.height / 3 },
      insert: true,
    });
    // Only on an explicit request, from the graph and selection of that moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paletteRequest]);

  useEffect(() => {
    if (!focusRequest || !selected) return;
    const n = nodeBySlug(graph, selected);
    if (n) void rf.setCenter((n.x ?? 0) + 120, (n.y ?? 0) + 60, { zoom: 1.1, duration: 300 });
    // Only on an explicit request, not on every selection change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusRequest]);

  useEffect(() => {
    if (!revealRequest || !selected || !wrapper.current) return;
    const n = nodeBySlug(graph, selected);
    if (!n) return;
    const r = wrapper.current.getBoundingClientRect();
    const tl = rf.flowToScreenPosition({ x: n.x ?? 0, y: n.y ?? 0 });
    const br = rf.flowToScreenPosition({ x: (n.x ?? 0) + 240, y: (n.y ?? 0) + 120 });
    const inside = tl.x >= r.left && tl.y >= r.top && br.x <= r.right && br.y <= r.bottom;
    if (!inside) void rf.setCenter((n.x ?? 0) + 120, (n.y ?? 0) + 60, { zoom: rf.getZoom(), duration: 200 });
    // Only on an explicit request, not on every selection change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revealRequest]);

  useEffect(() => {
    if (!hint) return;
    const t = setTimeout(() => setHint(null), 3500);
    return () => clearTimeout(t);
  }, [hint]);

  return (
    <div className="dz-flow" ref={wrapper} onDoubleClick={onDoubleClick}>
      <ReactFlow
        ariaLabelConfig={ariaLabels}
        nodes={[...frameNodes, ...nodes, ...ghosts] as AnyNode[]}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodesDelete={onNodesDelete}
        onEdgesDelete={onEdgesDelete}
        onConnect={onConnect}
        onConnectStart={onConnectStart}
        onConnectEnd={onConnectEnd}
        isValidConnection={isValidConnection}
        onPaneClick={() => onSelect(null)}
        onNodeContextMenu={onNodeContextMenu}
        onEdgeContextMenu={onEdgeContextMenu}
        onPaneContextMenu={onPaneContextMenu}
        zoomOnDoubleClick={false}
        deleteKeyCode={["Delete", "Backspace"]}
        minZoom={0.2}
        proOptions={{ hideAttribution: true }}
        // A step opens on release — a click or a tap — not on press, so
        // moving a step or drawing a wire from it leaves the inspector as
        // it was. Where the inspector is a sheet over the canvas, opening
        // on press would also cover the canvas mid-drag and abort it.
        selectNodesOnDrag={false}
        onMoveStart={(e) => {
          // Only a pan or zoom the author made; a programmatic one has no event.
          if (e) live?.onUserMove();
        }}
      >
        {/* dazyflow's two-level grid: a major line every 100, fine every 20. */}
        <Background id="fine" variant={BackgroundVariant.Lines} gap={20} className="dz-grid-fine" />
        <Background id="major" variant={BackgroundVariant.Lines} gap={100} className="dz-grid-major" />
        <Controls showInteractive={false}>
          <ControlButton onClick={addFrame} title={t.t("Group the selected steps in a frame")} aria-label={t.t("Add a frame")}>
            <FrameIcon size={ICON.sm} />
          </ControlButton>
        </Controls>
        <MiniMap pannable zoomable className="dz-minimap canvas__minimap" />
        {live && <LiveLayer live={live} wrapper={wrapper} />}
      </ReactFlow>

      {hint && (
        <div className="canvas__hint" role="status">
          {hint}
        </div>
      )}

      {menu && <CanvasContextMenu x={menu.x} y={menu.y} items={menu.items} onClose={() => setMenu(null)} />}

      {palette && (
        <QuickPalette
          graph={graph}
          from={palette.from}
          choosePort={!!palette.insert}
          at={palette.screen}
          {...(wrapper.current ? { bounds: wrapper.current.getBoundingClientRect() } : {})}
          onPick={pick}
          onClose={() => setPalette(null)}
        />
      )}
    </div>
  );
}
