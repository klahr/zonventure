import { createContext, useContext, useRef, useState } from "react";
import { ApiError, adventures, play } from "@zonventure/api";
import type { Media } from "@zonventure/graph";
import { useT } from "../i18n.tsx";

/** The adventure being edited, for uploads deep inside the inspector. */
export const AdventureSlug = createContext("");

/**
 * Pictures and sounds on a step or hint: upload, describe, reorder by
 * removing and re-adding, remove. The file is stored when it is picked;
 * the reference to it is part of the graph, so it is saved with Save like
 * any other edit — and undone with Undo.
 */
export function MediaEditor({ media, onChange }: { media: Media[] | undefined; onChange: (m: Media[] | undefined) => void }) {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const list = media ?? [];
  const set = (next: Media[]) => onChange(next.length ? next : undefined);

  const pick = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    setError(null);
    try {
      const added: Media[] = [];
      for (const f of Array.from(files)) {
        const r = await adventures.uploadMedia(slug, await downscale(f));
        if (!list.some((m) => m.id === r.id)) added.push({ id: r.id, kind: r.kind });
      }
      set([...list, ...added]);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  };

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
          <button type="button" className="link-btn" onClick={() => set(list.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            ×
          </button>
        </div>
      ))}
      <input ref={input} type="file" accept="image/*,audio/*" multiple hidden onChange={(e) => void pick(e.target.files)} />
      <button type="button" className="link-btn" disabled={busy || !slug} onClick={() => input.current?.click()}>
        {busy ? t.t("Uploading…") : "+ " + t.t("Add picture or sound")}
      </button>
      {error && <div className="alert alert--danger">{error}</div>}
    </div>
  );
}

/**
 * Shrink a photo in the browser before upload, as zv's console did: a
 * phone photo is often over the server's 5 MB cap, and the server resizes
 * to 512 px anyway, so sending the original only costs time on a venue's
 * wifi. Anything that cannot be decoded, or would not get smaller, goes
 * up as it is; GIF and SVG are left alone (animation, vector).
 */
async function downscale(file: File, maxDim = 1600, quality = 0.85): Promise<File> {
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
    if (longer <= maxDim) return file;
    const scale = maxDim / longer;
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
    if (!blob || blob.size >= file.size) return file;
    return new File([blob], file.name.replace(/\.[^.]+$/, "") + ".jpg", { type: "image/jpeg" });
  } finally {
    bitmap.close();
  }
}
