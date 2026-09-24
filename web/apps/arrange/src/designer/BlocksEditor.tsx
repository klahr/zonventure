import { useCallback, useRef, useState } from "react";
import { isFixed, mediaBlock, moveBlock, needsFile, newBlock, type Block, type BlockKind, type FileKind, type Graph, type Node } from "@zonventure/graph";
import { ChevronDown, ChevronRight, Eye, EyeOff, GripVertical, Plus, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { DzPortal, ICON, useAnchoredPop, useDismiss } from "./dz/ui.tsx";
import { useMediaUpload } from "./MediaEditor.tsx";
import { ACCEPT } from "./blocks/Media.tsx";
import { GROUPS, KINDS } from "./blocks/registry.tsx";
import { ShowEditor } from "./blocks/Show.tsx";
import { summarize } from "./blocks/summary.ts";
import type { Editor, T } from "./blocks/types.ts";

function body(b: Block, set: (b: Block) => void, t: T, textLabel: string, graph: Graph, node: Node) {
  const Body = KINDS[b.kind].editor as Editor<BlockKind>;
  return <Body block={b as never} set={set} t={t} textLabel={textLabel} graph={graph} node={node} />;
}

/**
 * What a puzzle, story or end shows, as blocks in order. Each is dragged
 * by its handle, or moved with the arrow keys while the handle has focus.
 * Any block may wait for a condition (the eye). Files are stored when
 * picked, like MediaEditor's.
 */
export function BlocksEditor({ blocks, node, graph, onChange, textLabel }: { blocks: Block[] | undefined; node: Node; graph: Graph; onChange: (b: Block[]) => void; textLabel: string }) {
  const t = useT();
  const list = blocks ?? [];
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const { busy, error, upload, ready } = useMediaUpload();
  // An upload outlives the render that started it.
  const latest = useRef({ list, onChange });
  latest.current = { list, onChange };
  // The kind the file being picked is for; null takes it from the file.
  const [fileFor, setFileFor] = useState<FileKind | null>(null);
  // The block whose handle is held, so only a drag from the handle moves
  // it, and where it would land.
  const [armed, setArmed] = useState<number | null>(null);
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);
  // Blocks whose show rule is open for editing, and blocks folded away.
  const [rules, setRules] = useState<Set<string>>(new Set());
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const fold = (id: string) =>
    setFolded((f) => {
      const n = new Set(f);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const allFolded = list.length > 0 && list.every((b) => folded.has(b.id));

  const set = (i: number, b: Block) => onChange(list.map((x, j) => (j === i ? b : x)));
  const move = (from: number, to: number) => onChange(moveBlock(list, from, to));

  const add = (kind: BlockKind) => {
    if (!needsFile(kind)) return onChange([...list, newBlock(list, kind)]);
    setFileFor(kind === "image" || kind === "audio" ? null : kind);
    if (input.current) input.current.accept = kind === "image" || kind === "audio" ? "image/*,audio/*" : (ACCEPT[kind] ?? "");
    input.current?.click();
  };

  const pick = async (files: FileList | null) => {
    const kind = fileFor;
    const added = await upload(files);
    if (input.current) input.current.value = "";
    if (!added.length) return;
    const { list: now, onChange: change } = latest.current;
    let next = now;
    for (const m of added) {
      // A film is only ever a video block; a picture for a torch stays a torch.
      const as: FileKind = m.kind === "video" ? "video" : kind && kind !== "video" && m.kind === "image" ? kind : m.kind;
      next = [...next, mediaBlock(next, m, as)];
    }
    change(next);
  };

  const drop = () => {
    if (drag) move(drag.from, drag.to);
    setDrag(null);
    setArmed(null);
  };

  return (
    <div className="blk-ed" ref={root}>
      {list.length > 1 && (
        <button type="button" className="link-btn blk-ed__fold-all" onClick={() => setFolded(allFolded ? new Set() : new Set(list.map((b) => b.id)))}>
          {allFolded ? t.t("Expand all") : t.t("Collapse all")}
        </button>
      )}
      {list.map((b, i) => {
        const kind = KINDS[b.kind];
        const Icon = kind.icon;
        const label = b.kind === "text" ? textLabel : t.t(kind.label);
        const target = drag && drag.from !== i && drag.to === i ? (drag.to < drag.from ? " is-drop-before" : " is-drop-after") : "";
        const ruleOpen = rules.has(b.id) || !!b.show;
        const isFolded = folded.has(b.id);
        return (
          <div
            key={b.id}
            className={"blk" + (drag?.from === i ? " is-dragging" : "") + (b.show ? " has-rule" : "") + (isFolded ? " is-folded" : "") + target}
            draggable={armed === i}
            onDragStart={(e) => {
              e.dataTransfer.effectAllowed = "move";
              e.dataTransfer.setData("text/plain", b.id);
              setDrag({ from: i, to: i });
            }}
            onDragOver={(e) => {
              if (!drag) return;
              e.preventDefault();
              if (drag.to !== i) setDrag({ ...drag, to: i });
            }}
            onDrop={(e) => {
              e.preventDefault();
              drop();
            }}
            onDragEnd={() => {
              setDrag(null);
              setArmed(null);
            }}
          >
            <div className="blk__head">
              <button
                type="button"
                className="blk__handle"
                aria-label={t.t("Move {{block}}: drag, or use the arrow keys", { block: label })}
                title={t.t("Drag to reorder")}
                onPointerDown={() => setArmed(i)}
                onPointerUp={() => setArmed(null)}
                onKeyDown={(e) => {
                  if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
                  e.preventDefault();
                  const to = e.key === "ArrowUp" ? i - 1 : i + 1;
                  move(i, to);
                  // Keep the focus on the handle as it moves.
                  requestAnimationFrame(() => root.current?.querySelectorAll<HTMLElement>(".blk__handle")[Math.max(0, Math.min(to, list.length - 1))]?.focus());
                }}
              >
                <GripVertical size={ICON.sm} aria-hidden="true" />
              </button>
              <button
                type="button"
                className="blk__fold"
                aria-expanded={!isFolded}
                aria-label={isFolded ? t.t("Expand {{block}}", { block: label }) : t.t("Collapse {{block}}", { block: label })}
                onClick={() => fold(b.id)}
              >
                {isFolded ? <ChevronRight size={ICON.sm} aria-hidden="true" /> : <ChevronDown size={ICON.sm} aria-hidden="true" />}
                <Icon size={ICON.sm} aria-hidden="true" className="blk__icon" />
                <span className="blk__label">{label}</span>
                {isFolded && <span className="blk__summary">{summarize(b, graph, t, node)}</span>}
              </button>
              <button
                type="button"
                className={"blk__rule" + (b.show ? " is-on" : "")}
                aria-pressed={ruleOpen}
                aria-label={b.show ? t.t("Shown only when a condition holds") : t.t("Show only when…")}
                title={b.show ? t.t("Shown only when a condition holds") : t.t("Show only when…")}
                onClick={() => {
                  if (isFolded) fold(b.id);
                  if (b.show) return;
                  setRules((r) => {
                    const n = new Set(r);
                    if (n.has(b.id)) n.delete(b.id);
                    else n.add(b.id);
                    return n;
                  });
                }}
              >
                {b.show ? <EyeOff size={ICON.sm} /> : <Eye size={ICON.sm} />}
              </button>
              {/* The step's title and points are always there: moved, never removed. */}
              {!isFixed(b.kind) && (
                <button type="button" className="icon-remove" onClick={() => onChange(list.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
                  <X size={ICON.sm} />
                </button>
              )}
            </div>
            {!isFolded && body(b, (next) => set(i, next), t, label, graph, node)}
            {!isFolded && ruleOpen && <ShowEditor block={b} set={(next) => set(i, next)} graph={graph} t={t} />}
          </div>
        );
      })}
      <input ref={input} type="file" multiple hidden onChange={(e) => void pick(e.target.files)} />
      <AddBlock onAdd={add} busy={busy} canUpload={ready} t={t} />
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}

/** "Add block": every kind, grouped, with what each is for. */
function AddBlock({ onAdd, busy, canUpload, t }: { onAdd: (k: BlockKind) => void; busy: boolean; canUpload: boolean; t: T }) {
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const { trigger, pop, style } = useAnchoredPop<HTMLButtonElement, HTMLDivElement>(open, "left");
  useDismiss(open, close, trigger, pop);
  const kinds = Object.entries(KINDS) as [BlockKind, (typeof KINDS)[BlockKind]][];
  return (
    <>
      <button ref={trigger} type="button" className="add-btn concept--content" aria-expanded={open} disabled={busy} onClick={() => setOpen((v) => !v)}>
        {!busy && <Plus size={ICON.sm} aria-hidden="true" />}
        {busy ? t.t("Uploading…") : t.t("Add block")}
        {!busy && <ChevronDown size={ICON.sm} aria-hidden="true" />}
      </button>
      {open && (
        <DzPortal>
          <div className="blk-menu" role="menu" ref={pop} style={style}>
            {GROUPS.map((g) => (
              <div key={g} className="blk-menu__group" role="group" aria-label={t.t(g)}>
                <span className="blk-menu__title">{t.t(g)}</span>
                {kinds
                  .filter(([k, d]) => d.group === g && !isFixed(k))
                  .map(([k, d]) => {
                    const Icon = d.icon;
                    const disabled = needsFile(k) && !canUpload;
                    return (
                      <button
                        key={k}
                        type="button"
                        role="menuitem"
                        className="blk-menu__item"
                        disabled={disabled}
                        onClick={() => {
                          setOpen(false);
                          onAdd(k);
                        }}
                      >
                        <Icon size={ICON.md} aria-hidden="true" />
                        <span className="blk-menu__name">{t.t(d.label)}</span>
                        <span className="blk-menu__about">{t.t(d.about)}</span>
                      </button>
                    );
                  })}
              </div>
            ))}
          </div>
        </DzPortal>
      )}
    </>
  );
}
