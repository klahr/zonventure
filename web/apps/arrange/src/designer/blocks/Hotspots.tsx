import { useState, type MouseEvent } from "react";
import { play } from "@zonventure/api";
import { fractionAt } from "@zonventure/play-ui";
import type { Spot } from "@zonventure/graph";
import { X } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import { MediaEditor } from "../MediaEditor.tsx";
import { AltInput } from "./Media.tsx";
import { withField, type Editor, type T as EditorT } from "./types.ts";
import "./chat-editor.css";
import "./pictures.css";

/**
 * A picture to click spots onto, and each spot's words: a hotspots
 * picture's details, a spotdiff's differences.
 */
function SpotPicker({ media, spots, setSpots, hint, placeholder, label, t }: { media: number; spots: Spot[]; setSpots: (next: Spot[]) => void; hint: string; placeholder: string; label: (n: number) => string; t: EditorT }) {
  const [fresh, setFresh] = useState<number | null>(null);
  const add = (e: MouseEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return;
    setFresh(spots.length);
    setSpots([...spots, { ...fractionAt(e.clientX, e.clientY, r), text: "" }]);
  };
  return (
    <>
      <div className="pic-ed__stage is-pick" onClick={add}>
        <img className="pic-ed__img" src={play.mediaURL(media)} alt="" draggable={false} />
        {spots.map((s, i) => (
          <span key={i} className="pic-ed__spot" style={{ left: `${s.x * 100}%`, top: `${s.y * 100}%` }} aria-hidden="true">
            {i + 1}
          </span>
        ))}
      </div>
      <p className="field__hint">{hint}</p>
      {spots.map((s, i) => (
        <div key={i} className="pic-ed__row">
          <span className="pic-ed__num" aria-hidden="true">
            {i + 1}
          </span>
          <textarea
            className="field__input"
            rows={2}
            value={s.text}
            placeholder={placeholder}
            aria-label={label(i + 1)}
            autoFocus={i === fresh}
            onChange={(e) => setSpots(spots.map((x, j) => (j === i ? { ...x, text: e.target.value } : x)))}
          />
          <button type="button" className="icon-remove" onClick={() => setSpots(spots.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
    </>
  );
}

export const HotspotsEditor: Editor<"hotspots"> = ({ block, set, t }) => (
  <>
    <SpotPicker
      media={block.media}
      spots={block.spots ?? []}
      setSpots={(next) => set(withField(block, "spots", next.length ? next : undefined))}
      hint={t.t("Click the picture to add a detail there.")}
      placeholder={t.t("What the team reads when it taps here")}
      label={(n) => t.t("Detail {{n}}", { n })}
      t={t}
    />
    <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
  </>
);

export const SpotDiffEditor: Editor<"spotdiff"> = ({ block, set, t }) => (
  <>
    <div className="blk-chat__avatar">
      <span className="field__label">{t.t("The other picture")}</span>
      <MediaEditor media={block.other ? [{ id: block.other, kind: "image" }] : undefined} single onChange={(m) => set(withField(block, "other", m?.[0]?.id))} />
    </div>
    <SpotPicker
      media={block.media}
      spots={block.spots ?? []}
      setSpots={(next) => set(withField(block, "spots", next.length ? next : undefined))}
      hint={t.t("Click the first picture where the two differ. Teams tap either picture there to find it.")}
      placeholder={t.t("Optional: what the team reads when it finds it")}
      label={(n) => t.t("Difference {{n}}", { n })}
      t={t}
    />
    <AltInput block={block} set={set} placeholder={t.t("Describe the pictures")} t={t} />
  </>
);
