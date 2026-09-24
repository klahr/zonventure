import { useState, type MouseEvent } from "react";
import { play } from "@zonventure/api";
import { fractionAt } from "@zonventure/play-ui";
import type { Spot } from "@zonventure/graph";
import { X } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import { AltInput } from "./Media.tsx";
import { withField, type Editor } from "./types.ts";
import "./pictures.css";

export const HotspotsEditor: Editor<"hotspots"> = ({ block, set, t }) => {
  const spots = block.spots ?? [];
  const [fresh, setFresh] = useState<number | null>(null);
  const setSpots = (next: Spot[]) => set(withField(block, "spots", next.length ? next : undefined));
  const add = (e: MouseEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return;
    setFresh(spots.length);
    setSpots([...spots, { ...fractionAt(e.clientX, e.clientY, r), text: "" }]);
  };
  return (
    <>
      <div className="pic-ed__stage is-pick" onClick={add}>
        <img className="pic-ed__img" src={play.mediaURL(block.media)} alt="" draggable={false} />
        {spots.map((s, i) => (
          <span key={i} className="pic-ed__spot" style={{ left: `${s.x * 100}%`, top: `${s.y * 100}%` }} aria-hidden="true">
            {i + 1}
          </span>
        ))}
      </div>
      <p className="field__hint">{t.t("Click the picture to add a detail there.")}</p>
      {spots.map((s, i) => (
        <div key={i} className="pic-ed__row">
          <span className="pic-ed__num" aria-hidden="true">
            {i + 1}
          </span>
          <textarea
            className="field__input"
            rows={2}
            value={s.text}
            placeholder={t.t("What the team reads when it taps here")}
            aria-label={t.t("Detail {{n}}", { n: i + 1 })}
            autoFocus={i === fresh}
            onChange={(e) => setSpots(spots.map((x, j) => (j === i ? { ...x, text: e.target.value } : x)))}
          />
          <button type="button" className="icon-remove" onClick={() => setSpots(spots.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <AltInput block={block} set={set} placeholder={t.t("Describe the picture")} t={t} />
    </>
  );
};
