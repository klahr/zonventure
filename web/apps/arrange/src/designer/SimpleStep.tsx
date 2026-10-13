import { useRef } from "react";
import { play } from "@zonventure/api";
import { blocksOf, mediaBlock, textBlock, updateNode, withBlocks, type Block, type Graph, type Matcher, type Node, type Output } from "@zonventure/graph";
import { Camera, Plus, SlidersHorizontal, Trash2, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { Field, ZoneSelect } from "./fields.tsx";
import { useMediaUpload } from "./MediaEditor.tsx";
import { isTouch } from "./device.ts";
import { retitle } from "./zones.ts";
import { Button, ICON } from "./dz/ui.tsx";
import { MarkdownArea } from "./MarkdownArea.tsx";

type Picture = Block & { kind: "image"; media: number; alt?: string };

/** The names a new step starts with, shown as an empty field to fill in. */
const DEFAULT_TITLES = new Set(["New puzzle", "New story"]);

/**
 * A puzzle or story on a phone, as four questions: what is it called, what
 * does the team see, what is the answer, and where. Everything else stays
 * in the full inspector, one tap away under More options, so a step made
 * here is an ordinary step there.
 */
export function SimpleStep({ node, graph, onChange, onGraphChange, onMore, onDelete }: {
  node: Node;
  graph: Graph;
  onChange: (n: Node) => void;
  onGraphChange: (g: Graph) => void;
  onMore: () => void;
  onDelete: () => void;
}) {
  const t = useT();
  const blocks = blocksOf(node);
  const text = blocks.find((b): b is Extract<Block, { kind: "text" }> => b.kind === "text" && !b.disabled);
  const pictures = blocks.filter((b): b is Picture => b.kind === "image");
  // Titles and points are laid out for the team by themselves.
  const others = blocks.filter((b) => b !== text && b.kind !== "image" && b.kind !== "title" && b.kind !== "points");

  const setBlocks = (next: Block[]) => onChange(withBlocks(node, next));
  const setText = (value: string) => setBlocks(text ? blocks.map((b) => (b === text ? { ...text, text: value } : b)) : [...blocks, textBlock(blocks, value)]);

  const zone = node.puzzle?.zone ?? node.story?.zone;
  const setZone = (slug: string | undefined) =>
    node.puzzle
      ? onChange({ ...node, puzzle: withZone(node.puzzle, slug) })
      : onChange({ ...node, story: withZone(node.story ?? {}, slug) });

  return (
    <div className="simple-step">
      <Field label={t.t("Name")}>
        <input
          className="field__input"
          value={DEFAULT_TITLES.has(node.title ?? "") ? "" : (node.title ?? "")}
          placeholder={node.type === "puzzle" ? t.t("e.g. The big oak") : t.t("e.g. Welcome")}
          onChange={(e) => onGraphChange(retitle(graph, node, e.target.value, [t.t("Place")]))}
        />
      </Field>

      <Field label={t.t("What the team reads")}>
        <MarkdownArea className="field__input" rows={4} value={text?.text ?? ""} placeholder={t.t("A clue, a question or a bit of story.")} onChange={(e) => setText(e.target.value)} />
      </Field>
      <Pictures node={node} pictures={pictures} setBlocks={setBlocks} />
      {others.length > 0 && <p className="field__hint">{t.t("This step shows {{n}} more blocks. Edit them under More options.", { n: others.length })}</p>}

      {node.puzzle && <Answers node={node} onChange={onChange} />}

      <ZoneSelect
        graph={graph}
        label={node.puzzle ? t.t("Where the team answers") : t.t("Where the team reads it")}
        value={zone}
        onChange={setZone}
        here={{
          name: DEFAULT_TITLES.has(node.title ?? "") ? "" : (node.title ?? ""),
          set: (zones, slug) =>
            onGraphChange(
              updateNode({ ...graph, zones }, node.slug, (n) =>
                n.puzzle ? { ...n, puzzle: withZone(n.puzzle, slug) } : { ...n, story: withZone(n.story ?? {}, slug) },
              ),
            ),
        }}
      />

      <div className="simple-step__foot">
        <Button variant="ghost" onClick={onMore}>
          <SlidersHorizontal size={ICON.sm} aria-hidden="true" />
          {t.t("More options")}
        </Button>
        <Button variant="danger" onClick={onDelete}>
          <Trash2 size={ICON.sm} aria-hidden="true" />
          {t.t("Delete step")}
        </Button>
      </div>
    </div>
  );
}

function withZone<T extends { zone?: string }>(cfg: T, zone: string | undefined): T {
  const { zone: _, ...rest } = cfg;
  return (zone ? { ...rest, zone } : rest) as T;
}

/** The step's pictures as thumbnails, with Add picture and, on a phone, Take photo. */
function Pictures({ node, pictures, setBlocks }: { node: Node; pictures: Picture[]; setBlocks: (b: Block[]) => void }) {
  const t = useT();
  const { busy, error, upload, ready } = useMediaUpload();
  const file = useRef<HTMLInputElement>(null);
  const camera = useRef<HTMLInputElement>(null);
  // An upload outlives the render that started it.
  const latest = useRef(node);
  latest.current = node;

  const pick = async (files: FileList | null) => {
    const added = await upload(files);
    if (file.current) file.current.value = "";
    if (camera.current) camera.current.value = "";
    let next = blocksOf(latest.current);
    for (const m of added) if (m.kind === "image") next = [...next, mediaBlock(next, m)];
    if (added.length) setBlocks(next);
  };

  return (
    <div className="simple-step__pics">
      {pictures.map((p) => (
        <div key={p.id} className="simple-step__pic">
          <img src={play.mediaURL(p.media)} alt={p.alt ?? ""} />
          <button type="button" className="icon-remove" aria-label={t.t("Remove picture")} onClick={() => setBlocks(blocksOf(latest.current).filter((b) => b.id !== p.id))}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <input ref={file} type="file" accept="image/*" multiple hidden onChange={(e) => void pick(e.target.files)} />
      <input ref={camera} type="file" accept="image/*" capture="environment" hidden onChange={(e) => void pick(e.target.files)} />
      <div className="simple-step__row">
        <button type="button" className="add-btn concept--content" disabled={busy || !ready} onClick={() => file.current?.click()}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {busy ? t.t("Uploading…") : t.t("Add picture")}
        </button>
        {isTouch() && (
          <button type="button" className="add-btn concept--content" disabled={busy || !ready} onClick={() => camera.current?.click()}>
            <Camera size={ICON.sm} aria-hidden="true" />
            {t.t("Take photo")}
          </button>
        )}
      </div>
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}

/**
 * The typed answers the first answer route accepts. A puzzle answered some
 * other way, or with anything beyond plain answers, points to More options
 * rather than showing half of what it does.
 */
function Answers({ node, onChange }: { node: Node; onChange: (n: Node) => void }) {
  const t = useT();
  const cfg = node.puzzle!;
  const outputs = cfg.outputs ?? [];
  const at = outputs.findIndex((o) => o.trigger === "answer");
  const route = outputs[at];
  const matchers = route?.matchers ?? [];
  const plain = !cfg.answer && route && matchers.every((m) => m.kind === "exact");
  if (!plain) {
    return (
      <Field label={t.t("Correct answer")}>
        <p className="field__hint">{t.t("This puzzle is answered in a way set under More options.")}</p>
      </Field>
    );
  }
  const values = matchers.length ? matchers.map((m) => (m.kind === "exact" ? (m.value ?? "") : "")) : [""];
  const set = (next: string[]) => {
    const o: Output = { ...route, matchers: next.map((value): Matcher => ({ kind: "exact", value })) };
    onChange({ ...node, puzzle: { ...cfg, outputs: outputs.map((x, i) => (i === at ? o : x)) } });
  };
  return (
    <Field label={t.t("Correct answer")} hint={t.t("Capital letters and extra spaces don't matter.")}>
      <div className="simple-step__answers">
        {values.map((v, i) => (
          <div key={i} className="simple-step__row">
            <input
              className="field__input"
              value={v}
              placeholder={i === 0 ? t.t("The correct answer") : t.t("Also accepted")}
              aria-label={i === 0 ? t.t("Correct answer") : t.t("Also accepted")}
              onChange={(e) => set(values.map((x, j) => (j === i ? e.target.value : x)))}
            />
            {i > 0 && (
              <button type="button" className="icon-remove" aria-label={t.t("Remove")} onClick={() => set(values.filter((_, j) => j !== i))}>
                <X size={ICON.sm} />
              </button>
            )}
          </div>
        ))}
        <button type="button" className="link-btn" onClick={() => set([...values, ""])}>
          {t.t("+ Also accept another answer")}
        </button>
      </div>
    </Field>
  );
}
