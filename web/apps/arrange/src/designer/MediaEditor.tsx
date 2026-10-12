import { createContext, useContext, useRef, useState } from "react";
import { ApiError, adventures, play } from "@zonventure/api";
import type { Media } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { Camera, Plus, X } from "lucide-react";
import { ICON } from "./dz/ui.tsx";
import { ImageDrop } from "../ui/imageDrop.tsx";
import { isTouch } from "./device.ts";

/** The adventure being edited, for uploads deep inside the inspector. */
export const AdventureSlug = createContext("");

/**
 * Pictures and sounds on a step or hint: upload, describe, reorder by
 * removing and re-adding, remove. The file is stored when it is picked;
 * the reference to it is part of the graph, so it is saved with Save like
 * any other edit — and undone with Undo.
 */
export function MediaEditor({
  media,
  onChange,
  single,
}: {
  media: Media[] | undefined;
  onChange: (m: Media[] | undefined) => void;
  /** Exactly one picture, such as a tile puzzle's: a new upload replaces it. */
  single?: boolean;
}) {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const input = useRef<HTMLInputElement>(null);
  const camera = useRef<HTMLInputElement>(null);
  const { busy, error, upload } = useMediaUpload();
  const list = media ?? [];
  const set = (next: Media[]) => onChange(next.length ? next : undefined);
  // An upload outlives the render that started it: add its files to the
  // step as it is when they arrive, not as it was when they were picked.
  const latest = useRef({ list, onChange });
  latest.current = { list, onChange };

  const pick = async (files: FileList | File[] | null) => {
    const added = await upload(files);
    if (input.current) input.current.value = "";
    if (camera.current) camera.current.value = "";
    if (!added.length) return;
    const { list: now, onChange: change } = latest.current;
    change(single ? added.slice(-1) : [...now, ...added.filter((a) => !now.some((m) => m.id === a.id))]);
  };

  if (single) {
    const m = list[0];
    return (
      <div className="media-ed">
        <ImageDrop
          shape="logo"
          src={m ? play.mediaURL(m.id) : undefined}
          label={m ? t.t("Replace picture") : t.t("Add picture")}
          accept="image/*"
          busy={busy}
          disabled={!slug}
          onFile={(f) => void pick([f])}
          onRemove={() => onChange(undefined)}
        />
        {m && (
          <input
            className="field__input"
            value={m.alt ?? ""}
            placeholder={t.t("Describe the picture")}
            aria-label={t.t("Description")}
            onChange={(e) => set([{ ...m, alt: e.target.value }])}
          />
        )}
        {error && <div className="alert alert--danger">{error}</div>}
      </div>
    );
  }

  return (
    <div className="media-ed">
      {list.map((m, i) => (
        <div key={m.id} className="media-ed__item">
          {m.kind === "image" ? (
            <img className="media-ed__thumb" src={play.mediaURL(m.id)} alt="" />
          ) : (
            <audio className="media-ed__audio" controls preload="none" src={play.mediaURL(m.id)} />
          )}
          <input
            className="field__input"
            value={m.alt ?? ""}
            placeholder={m.kind === "image" ? t.t("Describe the picture") : t.t("Describe the sound")}
            aria-label={t.t("Description")}
            onChange={(e) => set(list.map((x, j) => (j === i ? { ...x, alt: e.target.value } : x)))}
          />
          <button type="button" className="icon-remove" onClick={() => set(list.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <input ref={input} type="file" accept="image/*,audio/*" multiple hidden onChange={(e) => void pick(e.target.files)} />
      <button type="button" className="add-btn concept--content" disabled={busy || !slug} onClick={() => input.current?.click()}>
        {!busy && <Plus size={ICON.sm} aria-hidden="true" />}
        {busy ? t.t("Uploading…") : t.t("Add picture or sound")}
      </button>
      {isTouch() && (
        <>
          <input ref={camera} type="file" accept="image/*" capture="environment" hidden onChange={(e) => void pick(e.target.files)} />
          <button type="button" className="add-btn concept--content" disabled={busy || !slug} onClick={() => camera.current?.click()}>
            <Camera size={ICON.sm} aria-hidden="true" />
            {t.t("Take photo")}
          </button>
        </>
      )}
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}

/**
 * Uploads picked files to the adventure being edited, one at a time. It
 * resolves with what was stored, even when a later file failed; the
 * failure is in `error`.
 */
export function useMediaUpload() {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const upload = async (files: FileList | File[] | null): Promise<Media[]> => {
    const added: Media[] = [];
    if (!files?.length) return added;
    setBusy(true);
    setError(null);
    try {
      for (const f of Array.from(files)) {
        const r = await adventures.uploadMedia(slug, await downscale(f));
        // A PDF is a print material's, never a step's.
        if (r.kind === "pdf") {
          setError(t.t("Pick a picture, sound or video."));
          continue;
        }
        if (!added.some((m) => m.id === r.id)) added.push({ id: r.id, kind: r.kind });
      }
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
    } finally {
      setBusy(false);
    }
    return added;
  };
  return { busy, error, upload, ready: !!slug };
}

// What the server decodes. Anything else (an iPhone's HEIC) is re-encoded
// as JPEG even when it is already small enough.
const SERVER_IMAGE_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);

/**
 * Shrink a photo in the browser before upload, as zv's console did: a
 * phone photo is often over the server's 5 MB cap, and the server resizes
 * to 512 px anyway, so sending the original only costs time on a venue's
 * wifi. Anything that cannot be decoded, or would not get smaller, goes
 * up as it is; GIF and SVG are left alone (animation, vector).
 */
export async function downscale(file: File, maxDim = 1600, quality = 0.85): Promise<File> {
  if (!file.type.startsWith("image/") || file.type === "image/gif" || file.type === "image/svg+xml") return file;
  if (typeof createImageBitmap !== "function") return file;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    return file;
  }
  try {
    const longer = Math.max(bitmap.width, bitmap.height);
    const decodable = SERVER_IMAGE_TYPES.has(file.type);
    if (longer <= maxDim && decodable) return file;
    const scale = Math.min(1, maxDim / longer);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
    if (!blob || (decodable && blob.size >= file.size)) return file;
    return new File([blob], file.name.replace(/\.[^.]+$/, "") + ".jpg", { type: "image/jpeg" });
  } finally {
    bitmap.close();
  }
}
