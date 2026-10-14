import { Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { FIXED_KINDS, addMediaBlocks, blocksOf, freshBlockId, isFixed, moveBlock, needsFile, newBlock, updateNode, withBlocks, type Block, type BlockKind, type FileKind, type Graph, type Node, type Zone } from "@zonventure/graph";
import { Camera, ChevronDown, ChevronRight, Eye, EyeOff, GripVertical, Plus, Power, PowerOff, Sparkles, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { Button, ConfirmModal, DzPortal, ICON, useAnchoredPop, useDismiss } from "./dz/ui.tsx";
import { useMediaUpload } from "./MediaEditor.tsx";
import { isNarrow, isTouch } from "./device.ts";
import { ACCEPT } from "./blocks/Media.tsx";
import { GROUPS, KINDS } from "./blocks/registry.tsx";
import { ShowEditor } from "./blocks/Show.tsx";
import { AnimationEditor } from "./blocks/Animation.tsx";
import { summarize } from "./blocks/summary.ts";
import type { Editor, T } from "./blocks/types.ts";

function body(b: Block, set: (b: Block) => void, t: T, textLabel: string, graph: Graph, node: Node, onGraphChange?: (g: Graph) => void) {
  const Body = KINDS[b.kind].editor as Editor<BlockKind>;
  const withZones = onGraphChange
    ? (zones: Zone[], next: Block) => onGraphChange(updateNode({ ...graph, zones }, node.slug, (n) => withBlocks(n, blocksOf(n).map((x) => (x.id === next.id ? next : x)))))
    : undefined;
  return <Body block={b as never} set={set} t={t} textLabel={textLabel} graph={graph} node={node} withZones={withZones} />;
}

/**
 * What a puzzle, story or end shows, as blocks in order. Each is dragged
 * by its handle, or moved with the arrow keys while the handle has focus.
 * Any block may wait for a condition (the eye), or be switched off and
 * kept (the power button), the title and points too. Files are stored when
 * picked, like MediaEditor's.
 */
export function BlocksEditor({ blocks, node, graph, onChange, onGraphChange, textLabel }: { blocks: Block[] | undefined; node: Node; graph: Graph; onChange: (b: Block[]) => void; onGraphChange?: (g: Graph) => void; textLabel: string }) {
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
  // The kind a new block is being picked for, kept as an empty block if
  // the picker is cancelled; or the empty block a pick fills in.
  const pending = useRef<FileKind | null>(null);
  const fillFor = useRef<string | null>(null);
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    const onCancel = () => {
      const kind = pending.current;
      pending.current = null;
      fillFor.current = null;
      if (!kind) return;
      const { list: now, onChange: change } = latest.current;
      change([...now, { id: freshBlockId(now), kind, media: 0 } as Block]);
    };
    el.addEventListener("cancel", onCancel);
    return () => el.removeEventListener("cancel", onCancel);
  }, []);
  // A block being dragged by its handle: where it came from, where it
  // would land, and how far it has moved. The rest slide aside to show.
  const [drag, setDrag] = useState<{ from: number; to: number; dy: number; shift: number } | null>(null);
  // After a drop, the moved block glides from where it was let go.
  const settle = useRef<{ id: string; dy: number; moved: boolean } | null>(null);
  // Blocks whose show rule is open for editing, and blocks folded away.
  const [rules, setRules] = useState<Set<string>>(new Set());
  const [anims, setAnims] = useState<Set<string>>(() => new Set());
  const [folded, setFolded] = useState<Set<string>>(new Set());
  // The block whose delete waits for a yes.
  const [removing, setRemoving] = useState<Block | null>(null);
  const labelOf = (b: Block) => (b.kind === "text" ? textLabel : t.t(KINDS[b.kind].label));
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

  // The title and points the step may have but its author removed.
  const missing = (FIXED_KINDS[node.type] ?? []).filter((k) => !list.some((b) => b.kind === k));
  const add = (kind: BlockKind, fromCamera = false) => {
    // A title comes back where a title shows: at the top.
    if (kind === "title") return onChange([newBlock(list, kind), ...list]);
    if (!needsFile(kind)) return onChange([...list, newBlock(list, kind)]);
    setFileFor(kind === "image" || kind === "audio" ? null : kind);
    pending.current = kind;
    fillFor.current = null;
    if (input.current) {
      input.current.accept = fromCamera ? "image/*" : kind === "image" || kind === "audio" ? "image/*,audio/*" : (ACCEPT[kind] ?? "");
      if (fromCamera) input.current.setAttribute("capture", "environment");
      else input.current.removeAttribute("capture");
    }
    input.current?.click();
  };

  // An empty block's "Add picture": the file picked becomes its file.
  const fill = (b: Block) => {
    if (!needsFile(b.kind)) return;
    setFileFor(b.kind);
    pending.current = null;
    fillFor.current = b.id;
    if (input.current) {
      input.current.accept = ACCEPT[b.kind] ?? "";
      input.current.removeAttribute("capture");
    }
    input.current?.click();
  };

  const pick = async (files: FileList | null) => {
    const kind = fileFor;
    const target = fillFor.current;
    pending.current = null;
    fillFor.current = null;
    const added = await upload(files);
    if (input.current) input.current.value = "";
    if (!added.length) return;
    const { list: now, onChange: change } = latest.current;
    const m = added[0]!;
    if (target) return change(now.map((x) => (x.id === target ? ({ ...x, media: m.id, ...(m.alt ? { alt: m.alt } : {}) } as Block) : x)));
    change(addMediaBlocks(now, added, kind));
  };

  const startDrag = (e: React.PointerEvent<HTMLButtonElement>, from: number) => {
    if (e.button !== 0) return;
    const handle = e.currentTarget;
    const rows = Array.from(root.current?.querySelectorAll<HTMLElement>(":scope > .blk") ?? []);
    const rects = rows.map((r) => r.getBoundingClientRect());
    if (!rects[from]) return;
    e.preventDefault();
    // Moves keep coming to the handle even when the pointer leaves it.
    try {
      handle.setPointerCapture(e.pointerId);
    } catch {
      // A pointer the browser no longer tracks: the drag still works while over the handle.
    }
    const gap = rects.length > 1 ? rects[1]!.top - rects[0]!.bottom : 0;
    const shift = rects[from]!.height + gap;
    const mids = rects.map((r) => r.top + r.height / 2);
    const scroller = (root.current?.closest(".inspector-body") as HTMLElement | null) ?? document.scrollingElement;
    const startY = e.clientY;
    const startScroll = scroller?.scrollTop ?? 0;
    let y = e.clientY;
    let to = from;
    let frame = 0;
    const update = () => {
      const dy = y - startY + ((scroller?.scrollTop ?? 0) - startScroll);
      const centre = mids[from]! + dy;
      // The slot is the one whose middle the dragged block's middle has passed.
      to = from;
      while (to < mids.length - 1 && centre > mids[to + 1]!) to++;
      while (to > 0 && centre < mids[to - 1]!) to--;
      setDrag({ from, to, dy, shift });
    };
    // Near the top or bottom of the panel, scroll it along.
    const edge = () => {
      if (scroller) {
        const box = scroller === document.scrollingElement ? { top: 0, bottom: window.innerHeight } : scroller.getBoundingClientRect();
        const step = y < box.top + 48 ? -8 : y > box.bottom - 48 ? 8 : 0;
        if (step) {
          scroller.scrollTop += step;
          update();
        }
      }
      frame = requestAnimationFrame(edge);
    };
    frame = requestAnimationFrame(edge);
    const onMove = (ev: PointerEvent) => {
      y = ev.clientY;
      update();
    };
    const finish = (commit: boolean) => {
      cancelAnimationFrame(frame);
      handle.removeEventListener("pointermove", onMove);
      handle.removeEventListener("pointerup", onUp);
      handle.removeEventListener("pointercancel", onCancel);
      const dy = y - startY + ((scroller?.scrollTop ?? 0) - startScroll);
      if (commit && to !== from) {
        // Where the block sits in its new slot, relative to where it started.
        const slot = to > from ? rects[to]!.bottom - rects[from]!.bottom : rects[to]!.top - rects[from]!.top;
        settle.current = { id: list[from]!.id, dy: dy - slot, moved: true };
        move(from, to);
      } else settle.current = { id: list[from]!.id, dy, moved: false };
      setDrag(null);
    };
    const onUp = () => finish(true);
    const onCancel = () => finish(false);
    handle.addEventListener("pointermove", onMove);
    handle.addEventListener("pointerup", onUp);
    handle.addEventListener("pointercancel", onCancel);
    setDrag({ from, to: from, dy: 0, shift });
  };

  // Glide the dropped block from where it was let go into its place.
  useLayoutEffect(() => {
    const s = settle.current;
    if (!s || drag) return;
    settle.current = null;
    const rows = Array.from(root.current?.querySelectorAll<HTMLElement>(":scope > .blk") ?? []);
    const el = rows.find((r) => r.dataset.id === s.id);
    if (!el) return;
    // The blocks that stepped aside are already in their new places; they
    // must not slide again from where they were.
    const still = s.moved ? rows : [el];
    for (const r of still) r.style.transition = "none";
    if (s.dy && !matchMedia("(prefers-reduced-motion: reduce)").matches) el.style.transform = `translateY(${s.dy}px)`;
    void el.offsetHeight;
    for (const r of still) r.style.transition = "";
    el.style.transform = "";
  });

  return (
    <div className="blk-ed" ref={root}>
      {list.length > 1 && (
        <Button variant="link" className="blk-ed__fold-all" onClick={() => setFolded(allFolded ? new Set() : new Set(list.map((b) => b.id)))}>
          {allFolded ? t.t("Expand all") : t.t("Collapse all")}
        </Button>
      )}
      {list.map((b, i) => {
        const kind = KINDS[b.kind];
        const Icon = kind.icon;
        const label = labelOf(b);
        const ruleOpen = rules.has(b.id) || !!b.show;
        const animOpen = anims.has(b.id);
        const isFolded = folded.has(b.id);
        // While dragging: the held block follows the pointer, and those
        // between where it was and where it would go step aside.
        let offset = 0;
        if (drag) {
          if (i === drag.from) offset = drag.dy;
          else if (drag.from < drag.to && i > drag.from && i <= drag.to) offset = -drag.shift;
          else if (drag.from > drag.to && i >= drag.to && i < drag.from) offset = drag.shift;
        }
        return (
          <div
            key={b.id}
            data-id={b.id}
            className={"blk" + (drag ? (drag.from === i ? " is-lifted" : " is-shifting") : "") + (b.show ? " has-rule" : "") + (b.disabled ? " is-disabled" : "") + (isFolded ? " is-folded" : "")}
            style={offset ? { transform: `translateY(${offset}px)` } : undefined}
          >
            <div className="blk__head">
              <button
                type="button"
                className="blk__handle"
                aria-label={t.t("Move {{block}}: drag, or use the arrow keys", { block: label })}
                title={t.t("Drag to reorder")}
                onPointerDown={(e) => startDrag(e, i)}
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
              <button
                type="button"
                className={"blk__rule" + (b.animation ? " is-on" : "")}
                aria-pressed={animOpen}
                aria-label={b.animation ? t.t("Animated: change how it moves") : t.t("Animate…")}
                title={b.animation ? t.t("Animated: change how it moves") : t.t("Animate…")}
                onClick={() => {
                  if (isFolded) fold(b.id);
                  setAnims((r) => {
                    const n = new Set(r);
                    if (n.has(b.id)) n.delete(b.id);
                    else n.add(b.id);
                    return n;
                  });
                }}
              >
                <Sparkles size={ICON.sm} />
              </button>
              <button
                type="button"
                className={"blk__rule" + (b.disabled ? " is-on" : "")}
                aria-pressed={!!b.disabled}
                aria-label={b.disabled ? t.t("Switched off: turn {{block}} on", { block: label }) : t.t("Turn {{block}} off", { block: label })}
                title={b.disabled ? t.t("Switched off: the team does not see it") : t.t("Turn off, keeping it for later")}
                onClick={() => {
                  const { disabled: _, ...rest } = b;
                  set(i, b.disabled ? (rest as Block) : { ...b, disabled: true });
                }}
              >
                {b.disabled ? <PowerOff size={ICON.sm} /> : <Power size={ICON.sm} />}
              </button>
              <Button variant="ghost" size="icon" className="danger" onClick={() => setRemoving(b)} aria-label={t.t("Remove")}>
                <X size={ICON.sm} />
              </Button>
            </div>
            {!isFolded && (needsFile(b.kind) && !(b as { media?: number }).media ? (
              <button type="button" className="add-btn" onClick={() => fill(b)} disabled={busy || !ready}>
                <Plus size={ICON.sm} aria-hidden="true" />
                {ACCEPT[b.kind]?.startsWith("audio") ? t.t("Add sound") : ACCEPT[b.kind]?.startsWith("video") ? t.t("Add film") : t.t("Add picture")}
              </button>
            ) : body(b, (next) => set(i, next), t, label, graph, node, onGraphChange))}
            {!isFolded && ruleOpen && <ShowEditor block={b} set={(next) => set(i, next)} graph={graph} t={t} />}
            {!isFolded && animOpen && <AnimationEditor block={b} set={(next) => set(i, next)} t={t} />}
          </div>
        );
      })}
      <input ref={input} type="file" multiple hidden onChange={(e) => void pick(e.target.files)} />
      {removing && (
        <ConfirmModal
          title={t.t("Delete {{block}}?", { block: labelOf(removing) })}
          message={t.t("The team no longer sees it. To keep it for later, turn it off instead.")}
          confirmLabel={t.t("Delete")}
          danger
          onConfirm={() => {
            onChange(list.filter((b) => b.id !== removing.id));
            setRemoving(null);
          }}
          onCancel={() => setRemoving(null)}
        />
      )}
      <AddBlock onAdd={add} missing={missing} busy={busy} canUpload={ready} t={t} />
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}

/** The kinds a phone's Add block lists first; More blocks shows the rest. */
const COMMON = new Set<BlockKind>(["text", "image", "audio", "quiz", "compass", "map"]);

/** "Add block": every kind, grouped, with what each is for. */
function AddBlock({ onAdd, missing, busy, canUpload, t }: { onAdd: (k: BlockKind, fromCamera?: boolean) => void; missing: BlockKind[]; busy: boolean; canUpload: boolean; t: T }) {
  const [open, setOpen] = useState(false);
  // On a phone the menu opens on the common kinds, a screen's worth.
  const [everything, setEverything] = useState(false);
  const short = isNarrow() && !everything;
  const close = useCallback(() => setOpen(false), []);
  const { trigger, pop, style } = useAnchoredPop<HTMLButtonElement, HTMLDivElement>(open, "left");
  useDismiss(open, close, trigger, pop);
  const kinds = (Object.entries(KINDS) as [BlockKind, (typeof KINDS)[BlockKind]][]).filter(([k]) => !short || COMMON.has(k) || missing.includes(k));
  return (
    <>
      <button ref={trigger} type="button" className="add-btn concept--content" aria-expanded={open} disabled={busy} onClick={() => {
          setEverything(false);
          setOpen((v) => !v);
        }}>
        {!busy && <Plus size={ICON.sm} aria-hidden="true" />}
        {busy ? t.t("Uploading…") : t.t("Add block")}
        {!busy && <ChevronDown size={ICON.sm} aria-hidden="true" />}
      </button>
      {open && (
        <DzPortal>
          <div className="blk-menu" role="menu" ref={pop} style={style}>
            {GROUPS.filter((g) => kinds.some(([k, d]) => d.group === g && (!isFixed(k) || missing.includes(k)))).map((g) => (
              <div key={g} className="blk-menu__group" role="group" aria-label={t.t(g)}>
                <span className="blk-menu__title">{t.t(g)}</span>
                {kinds
                  .filter(([k, d]) => d.group === g && (!isFixed(k) || missing.includes(k)))
                  .map(([k, d]) => {
                    const Icon = d.icon;
                    const disabled = needsFile(k) && !canUpload;
                    return (
                      <Fragment key={k}>
                        <button
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
                        {k === "image" && isTouch() && (
                          <button
                            type="button"
                            role="menuitem"
                            className="blk-menu__item"
                            disabled={disabled}
                            onClick={() => {
                              setOpen(false);
                              onAdd("image", true);
                            }}
                          >
                            <Camera size={ICON.md} aria-hidden="true" />
                            <span className="blk-menu__name">{t.t("Take photo")}</span>
                            <span className="blk-menu__about">{t.t("Opens the camera and adds the photo as a picture.")}</span>
                          </button>
                        )}
                      </Fragment>
                    );
                  })}
              </div>
            ))}
            {short && (
              <button type="button" role="menuitem" className="blk-menu__item blk-menu__more" onClick={() => setEverything(true)}>
                <ChevronDown size={ICON.md} aria-hidden="true" />
                <span className="blk-menu__name">{t.t("More blocks")}</span>
                <span className="blk-menu__about">{t.t("Notes, props, gadgets and game pieces")}</span>
              </button>
            )}
          </div>
        </DzPortal>
      )}
    </>
  );
}
