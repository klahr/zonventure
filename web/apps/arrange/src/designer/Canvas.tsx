import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  applyEdgeChanges,
  applyNodeChanges,
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
  connect,
  connectionError,
  firstPort,
  moveNode,
  nodeBySlug,
  nodeFromPreset,
  reconcileByID,
  removeNode,
  disconnect,
  sameData,
  samePosition,
  type DragFrom,
  type Graph,
  type Issue,
} from "@zonventure/graph";
import { NodeCard, type CardNode } from "./NodeCard.tsx";
import { QuickPalette, type PaletteChoice } from "./QuickPalette.tsx";

const nodeTypes = { zv: NodeCard };

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
}

interface PendingDrag {
  nodeId: string;
  handleId: string | null;
  handleType: "source" | "target";
}

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
export function Canvas({ graph, issues, selected, onChange, onSelect, paletteRequest, focusRequest }: Props) {
  const rf = useReactFlow();
  const [nodes, setNodes] = useState<CardNode[]>([]);
  const [edges, setEdges] = useState<RFEdge[]>([]);
  const [palette, setPalette] = useState<{ from: DragFrom | null; screen: { x: number; y: number } } | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const pending = useRef<PendingDrag | null>(null);
  const connected = useRef(false);
  const wrapper = useRef<HTMLDivElement>(null);

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
          samePosition(existing.position, { x: n.x ?? 0, y: n.y ?? 0 }),
        build: (n, previous) => ({
          id: n.slug,
          type: "zv" as const,
          position: { x: n.x ?? 0, y: n.y ?? 0 },
          data: { node: n, issues: issuesBySlug.get(n.slug) ?? [] },
          selected: n.slug === selected,
          // Keep React Flow's measurement so handles do not jump.
          ...(previous?.measured ? { measured: previous.measured } : {}),
          deletable: n.type !== "start",
        }),
      }).items,
    );
  }, [graph.nodes, issuesBySlug, selected]);

  useEffect(() => {
    setEdges((prev) => {
      const selectedIDs = new Set(prev.filter((e) => e.selected).map((e) => e.id));
      return graph.edges.map((e) => {
        const id = `${e.from}:${e.from_port}`;
        const bad = issues.some((i) => i.node === e.from && i.port === e.from_port);
        return {
          id,
          source: e.from,
          sourceHandle: e.from_port,
          target: e.to,
          targetHandle: "in",
          selected: selectedIDs.has(id),
          className: bad ? "edge--bad" : "",
        };
      });
    });
  }, [graph.edges, issues]);

  const onNodesChange = useCallback(
    (changes: NodeChange<CardNode>[]) => {
      setNodes((ns) => applyNodeChanges(changes, ns));
      let next = graph;
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
    [graph, onChange, onSelect],
  );

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    setEdges((es) => applyEdgeChanges(changes, es));
  }, []);

  const onNodesDelete = useCallback(
    (deleted: CardNode[]) => {
      let next = graph;
      for (const n of deleted) next = removeNode(next, n.id);
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
      const from: DragFrom =
        start.handleType === "source"
          ? { side: "output", node, port: start.handleId ?? "" }
          : { side: "input", node };
      const pt =
        "clientX" in e
          ? { x: e.clientX, y: e.clientY }
          : { x: e.changedTouches[0]?.clientX ?? 0, y: e.changedTouches[0]?.clientY ?? 0 };
      setPalette({ from, screen: pt });
    },
    [graph],
  );

  const pick = useCallback(
    (choice: PaletteChoice) => {
      if (!palette) return;
      const { from, screen } = palette;
      setPalette(null);

      if (choice.kind === "existing") {
        if (from?.side === "output") onChange(connect(graph, from.node.slug, from.port, choice.node.slug));
        onSelect(choice.node.slug);
        return;
      }

      // Place the card so its input handle lands under the cursor, as if
      // the wire had been dropped straight onto it.
      const flow = rf.screenToFlowPosition(screen);
      const at = from?.side === "input" ? { x: flow.x - 240, y: flow.y - 40 } : { x: flow.x, y: flow.y - 40 };
      const node = nodeFromPreset(graph, choice.preset, at);
      let next = addNode(graph, node);
      if (from?.side === "output") next = connect(next, from.node.slug, from.port, node.slug);
      if (from?.side === "input") {
        const p = firstPort(node);
        if (p) next = connect(next, node.slug, p, from.node.slug);
      }
      onChange(next);
      onSelect(node.slug);
    },
    [palette, graph, onChange, onSelect, rf],
  );

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
    setPalette({ from: null, screen: { x: r.left + r.width / 2 - 170, y: r.top + r.height / 3 } });
  }, [paletteRequest]);

  useEffect(() => {
    if (!focusRequest || !selected) return;
    const n = nodeBySlug(graph, selected);
    if (n) void rf.setCenter((n.x ?? 0) + 120, (n.y ?? 0) + 60, { zoom: 1.1, duration: 300 });
    // Only on an explicit request, not on every selection change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusRequest]);

  useEffect(() => {
    if (!hint) return;
    const t = setTimeout(() => setHint(null), 3500);
    return () => clearTimeout(t);
  }, [hint]);

  return (
    <div className="canvas" ref={wrapper} onDoubleClick={onDoubleClick}>
      <ReactFlow
        nodes={nodes}
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
        zoomOnDoubleClick={false}
        deleteKeyCode={["Delete", "Backspace"]}
        fitView
        fitViewOptions={{ padding: 0.2, maxZoom: 1.1 }}
        minZoom={0.2}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={24} />
        <Controls showInteractive={false} />
        <MiniMap pannable zoomable className="canvas__minimap" />
      </ReactFlow>

      {hint && (
        <div className="canvas__hint" role="status">
          {hint}
        </div>
      )}

      {palette && (
        <QuickPalette
          graph={graph}
          from={palette.from}
          at={palette.screen}
          {...(wrapper.current ? { bounds: wrapper.current.getBoundingClientRect() } : {})}
          onPick={pick}
          onClose={() => setPalette(null)}
        />
      )}
    </div>
  );
}
