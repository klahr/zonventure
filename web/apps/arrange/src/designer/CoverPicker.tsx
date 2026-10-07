import { useContext, useRef, useState } from "react";
import { adventures, type play } from "@zonventure/api";
import { ImagePlus, Trash2 } from "lucide-react";
import { useT } from "../i18n.tsx";
import { ICON } from "./dz/ui.tsx";
import { AdventureSlug, downscale } from "./MediaEditor.tsx";

/**
 * The look's cover picture: a 16:9 picture across the top of a hosted
 * event's join page and the players' welcome, and on the printed cover
 * of a quiz trail, in place of the starter theme's. It is cut to 16:9
 * about its middle; titles sit on its left side.
 */
export function CoverPicker({ brand, setStyle }: { brand: play.Brand; setStyle: (patch: { cover?: number | undefined }) => void }) {
  const t = useT();
  const slug = useContext(AdventureSlug);
  const file = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cover = brand.style?.cover;

  const upload = async (f: File) => {
    setUploading(true);
    setError(null);
    try {
      const r = await adventures.uploadMedia(slug, await downscale(f, 1600));
      setStyle({ cover: r.id });
    } catch (e) {
      setError(e instanceof Error ? e.message : t.t("Could not upload the picture."));
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="look__backdrop">
      <h4>{t.t("Cover picture")}</h4>
      {cover ? <img className="look__cover" src={`/media/${cover}`} alt="" /> : null}
      <div className="look__actions">
        <button type="button" className="btn btn--ghost btn--sm" disabled={!slug || uploading} onClick={() => file.current?.click()}>
          <ImagePlus size={ICON.sm} aria-hidden />
          {uploading ? t.t("Uploading…") : cover ? t.t("Replace picture") : t.t("Add a cover picture")}
        </button>
        {cover ? (
          <button type="button" className="btn btn--ghost btn--sm" disabled={uploading} onClick={() => setStyle({ cover: undefined })}>
            <Trash2 size={ICON.sm} aria-hidden />
            {t.t("Remove")}
          </button>
        ) : null}
      </div>
      <input
        ref={file}
        type="file"
        accept="image/*"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (f) void upload(f);
        }}
      />
      {error && <div className="alert alert--danger">{error}</div>}
      <p className="field__hint">{t.t("Shown across the top of the join page and the players' welcome, and on a quiz trail's printed cover. Landscape (16:9) works best, with the subject on the right: titles sit on the left.")}</p>
    </div>
  );
}
