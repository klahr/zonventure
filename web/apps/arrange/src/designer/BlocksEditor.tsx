import { useRef, useState, type ReactNode } from "react";
import { play } from "@zonventure/api";
import { MAP_ZOOM, mediaBlock, moveBlock, placeBlock, textBlock, type Block, type BlockKind, type Graph } from "@zonventure/graph";
import { CompassFace, FitMap } from "@zonventure/play-ui";
import { Compass, GripVertical, Image, Map as MapIcon, Plus, Type, Volume2, X, type LucideIcon } from "lucide-react";
import type { I18n as T } from "@zonventure/i18n";
import { useT } from "../i18n.tsx";
import { ICON } from "./dz/ui.tsx";
import { useMediaUpload } from "./MediaEditor.tsx";
import { ZoneSelect } from "./fields.tsx";

type Of<K extends BlockKind> = Block & { kind: K };
type Body<K extends BlockKind> = (p: { block: Of<K>; set: (b: Of<K>) => void; t: T; textLabel: string; graph: Graph }) => ReactNode;

/**
 * The kinds of block, and how each is edited. A new kind (a compass, a
 * map) is a type in @zonventure/graph, an entry here, and a renderer in
 * play-ui's Blocks.
 */
const KINDS: { [K in BlockKind]: { label: string; icon: LucideIcon; body: Body<K> } } = {
  text: {
    label: "Text",
    icon: Type,
    body: ({ block, set, t, textLabel }) => (
      <textarea
        className="field__input"
        rows={4}
        value={block.text}
        placeholder={t.t("Markdown. This is what the team reads.")}
        aria-label={textLabel}
        onChange={(e) => set({ ...block, text: e.target.value })}
      />
    ),
  },
  image: {
    label: "Picture",
    icon: Image,
    body: ({ block, set, t }) => (
      <div className="media-ed__item">
        <img className="media-ed__thumb" src={play.mediaURL(block.media)} alt="" />
        <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
      </div>
    ),
  },
  audio: {
    label: "Sound",
    icon: Volume2,
    body: ({ block, set, t }) => (
      <div className="media-ed__item">
        <audio className="media-ed__audio" controls preload="none" src={play.mediaURL(block.media)} />
        <AltInput block={block} set={set} placeholder={t.t("Describe the sound")} t={t} />
      </div>
    ),
  },
  compass: {
    label: "Compass",
    icon: Compass,
    body: ({ block, set, t, graph }) => {
      const zone = graph.zones?.find((z) => z.slug === block.zone);
      return (
        <>
          <PlaceSelect block={block} set={set} t={t} graph={graph} />
          <label className="check">
            <input
              type="checkbox"
              checked={!block.hide_distance}
              onChange={(e) => {
                const { hide_distance: _, ...rest } = block;
                set(e.target.checked ? rest : { ...rest, hide_distance: true });
              }}
            />
            <span>{t.t("Show the distance")}</span>
          </label>
          {zone && (
            <div className="blk__preview" aria-label={t.t("Preview")}>
              <CompassFace
                name={zone.name || zone.slug}
                angle={45}
                northUp={false}
                t={(k, v) => t.t(k, v)}
                distance={block.hide_distance ? null : t.t("About {{m}} m away", { m: 250 })}
              />
            </div>
          )}
          <p className="field__hint">{t.t("Points the phone at the zone. Without a compass on the phone, north is up.")}</p>
        </>
      );
    },
  },
  map: {
    label: "Map",
    icon: MapIcon,
    body: ({ block, set, t, graph }) => {
      const zone = graph.zones?.find((z) => z.slug === block.zone);
      return (
        <>
          <PlaceSelect block={block} set={set} t={t} graph={graph} />
          <label className="check">
            <input
              type="checkbox"
              checked={!block.zoom}
              onChange={(e) => {
                const { zoom: _, ...rest } = block;
                set(e.target.checked ? rest : { ...rest, zoom: 16 });
              }}
            />
            <span>{t.t("Fit the map to the zone")}</span>
          </label>
          {block.zoom !== undefined && (
            <label className="field blk__zoom">
              <span className="field__label">{t.t("Zoom: {{n}}", { n: block.zoom })}</span>
              <input
                type="range"
                min={MAP_ZOOM.min}
                max={MAP_ZOOM.max}
                value={block.zoom}
                onChange={(e) => set({ ...block, zoom: Number(e.target.value) })}
              />
              <span className="blk__zoom-ends" aria-hidden="true">
                <span>{t.t("Wider")}</span>
                <span>{t.t("Closer")}</span>
              </span>
            </label>
          )}
          {zone && <FitMap className="blk__preview" zone={zone} zoom={block.zoom} />}
          <p className="field__hint">
            {block.zoom ? t.t("The team sees itself on the map whenever it is inside the frame.") : t.t("The team sees itself on the map when it is within 3 km.")}
          </p>
        </>
      );
    },
  },
};

function PlaceSelect<B extends Of<"compass" | "map">>({ block, set, t, graph }: { block: B; set: (b: B) => void; t: T; graph: Graph }) {
  return <ZoneSelect graph={graph} label={t.t("Zone")} empty={t.t("Choose a zone")} value={block.zone || undefined} onChange={(zone) => set({ ...block, zone: zone ?? "" })} />;
}

function AltInput<B extends Of<"image" | "audio">>({ block, set, placeholder, t }: { block: B; set: (b: B) => void; placeholder: string; t: T }) {
  return (
    <input
      className="field__input"
      value={block.alt ?? ""}
      placeholder={placeholder}
      aria-label={t.t("Description")}
      onChange={(e) => {
        const { alt: _, ...rest } = block;
        set((e.target.value ? { ...rest, alt: e.target.value } : rest) as B);
      }}
    />
  );
}

function body(b: Block, set: (b: Block) => void, t: T, textLabel: string, graph: Graph): ReactNode {
  const Body = KINDS[b.kind].body as Body<BlockKind>;
  return <Body block={b as never} set={set} t={t} textLabel={textLabel} graph={graph} />;
}

/**
 * What a puzzle, story or end shows, as blocks in order. Each is dragged
 * by its handle, or moved with the arrow keys while the handle has focus.
 * Pictures and sounds are stored when picked, like MediaEditor's.
 */
export function BlocksEditor({ blocks, graph, onChange, textLabel }: { blocks: Block[] | undefined; graph: Graph; onChange: (b: Block[]) => void; textLabel: string }) {
  const t = useT();
  const list = blocks ?? [];
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const { busy, error, upload, ready } = useMediaUpload();
  // An upload outlives the render that started it.
  const latest = useRef({ list, onChange });
  latest.current = { list, onChange };
  // The block whose handle is held, so only a drag from the handle moves
  // it, and where it would land.
  const [armed, setArmed] = useState<number | null>(null);
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);

  const set = (i: number, b: Block) => onChange(list.map((x, j) => (j === i ? b : x)));
  const move = (from: number, to: number) => onChange(moveBlock(list, from, to));

  const pick = async (files: FileList | null) => {
    const added = await upload(files);
    if (input.current) input.current.value = "";
    if (!added.length) return;
    const { list: now, onChange: change } = latest.current;
    let next = now;
    for (const m of added) next = [...next, mediaBlock(next, m)];
    change(next);
  };

  const drop = () => {
    if (drag) move(drag.from, drag.to);
    setDrag(null);
    setArmed(null);
  };

  return (
    <div className="blk-ed" ref={root}>
      {list.map((b, i) => {
        const kind = KINDS[b.kind];
        const Icon = kind.icon;
        const label = b.kind === "text" ? textLabel : t.t(kind.label);
        const target = drag && drag.from !== i && drag.to === i ? (drag.to < drag.from ? " is-drop-before" : " is-drop-after") : "";
        return (
          <div
            key={b.id}
            className={"blk" + (drag?.from === i ? " is-dragging" : "") + target}
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
              <Icon size={ICON.sm} aria-hidden="true" className="blk__icon" />
              <span className="blk__label">{label}</span>
              <button type="button" className="icon-remove" onClick={() => onChange(list.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
                <X size={ICON.sm} />
              </button>
            </div>
            {body(b, (next) => set(i, next), t, label, graph)}
          </div>
        );
      })}
      <div className="blk-ed__add">
        <button type="button" className="add-btn concept--content" onClick={() => onChange([...list, textBlock(list)])}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add text")}
        </button>
        <input ref={input} type="file" accept="image/*,audio/*" multiple hidden onChange={(e) => void pick(e.target.files)} />
        <button type="button" className="add-btn concept--content" disabled={busy || !ready} onClick={() => input.current?.click()}>
          {!busy && <Plus size={ICON.sm} aria-hidden="true" />}
          {busy ? t.t("Uploading…") : t.t("Add picture or sound")}
        </button>
        <button type="button" className="add-btn concept--place" onClick={() => onChange([...list, placeBlock(list, "compass")])}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add compass")}
        </button>
        <button type="button" className="add-btn concept--place" onClick={() => onChange([...list, placeBlock(list, "map")])}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add map")}
        </button>
      </div>
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}
