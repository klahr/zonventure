import { withField, type Editor } from "./types.ts";
import "./pictures.css";
import { MarkdownArea } from "../MarkdownArea.tsx";

const POINTS = ["N", "NE", "E", "SE", "S", "SW", "W", "NW"];

/** The compass point nearest a heading: 270 is W. */
const pointOf = (deg: number) => POINTS[Math.round(deg / 45) % 8]!;

export const BearingEditor: Editor<"bearing"> = ({ block, set, t }) => {
  const heading = block.heading ?? 0;
  const at = (deg: number, r: number) => ({ x: Math.sin((deg * Math.PI) / 180) * r, y: -Math.cos((deg * Math.PI) / 180) * r });
  const tip = at(heading, 30);
  return (
    <>
      <div className="blk-bearing">
        <svg className="blk-bearing__dial" viewBox="-50 -50 100 100" role="img" aria-label={t.t("Points {{deg}}° ({{point}})", { deg: heading, point: pointOf(heading) })}>
          <circle r="46" className="blk-bearing__ring" />
          {["N", "E", "S", "W"].map((p, i) => (
            <text key={p} className="blk-bearing__mark" x={at(i * 90, 36).x} y={at(i * 90, 36).y + 4} textAnchor="middle">
              {p}
            </text>
          ))}
          <line className="blk-bearing__needle" x1="0" y1="0" x2={tip.x} y2={tip.y} />
          <circle r="3" className="blk-bearing__hub" />
        </svg>
        <label className="field blk__zoom">
          <span className="field__label">{t.t("Heading: {{deg}}° ({{point}})", { deg: heading, point: pointOf(heading) })}</span>
          <input type="range" min={0} max={359} value={heading} onChange={(e) => set(withField(block, "heading", Number(e.target.value) || undefined))} />
          <span className="blk__zoom-ends" aria-hidden="true">
            <span>N</span>
            <span>E</span>
            <span>S</span>
            <span>W</span>
            <span>N</span>
          </span>
        </label>
      </div>
      <MarkdownArea
        className="field__input"
        rows={3}
        value={block.text ?? ""}
        placeholder={t.t("Markdown. What the team reads once the phone points the right way.")}
        aria-label={t.t("Text when done")}
        onChange={(e) => set(withField(block, "text", e.target.value))}
      />
      <p className="field__hint">{t.t("The team turns until the top of the phone points that way, and holds it there for a moment. Say what to face: “Face the church tower.”")}</p>
    </>
  );
};
