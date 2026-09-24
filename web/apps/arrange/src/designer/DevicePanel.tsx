import type { Motion } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";

/** Ways to hold a phone worth one click: the level's two targets, and holding it to read. */
const POSES: { label: string; motion: Omit<Motion, "heading"> }[] = [
  { label: "Flat on a table", motion: { beta: 0, gamma: 0 } },
  { label: "Held to read", motion: { beta: 45, gamma: 0 } },
  { label: "Upright", motion: { beta: 90, gamma: 0 } },
  { label: "Aimed 30° up", motion: { beta: 120, gamma: 0 } },
];

/**
 * The simulated phone's heading and tilt, for trying compass and level
 * blocks at a desk. The values are what DeviceOrientationEvent reports,
 * so the blocks see exactly what a phone would send.
 */
export function DevicePanel({ motion, onChange }: { motion: Motion; onChange: (m: Motion) => void }) {
  const t = useT();
  const slider = (key: keyof Motion, label: string, min: number, max: number, unit = "°") => (
    <label className="field sim__slider">
      <span className="field__label">
        {t.t(label)} <span className="sim__value">{Math.round(motion[key])}{unit}</span>
      </span>
      <input type="range" min={min} max={max} value={motion[key]} onChange={(e) => onChange({ ...motion, [key]: Number(e.target.value) })} />
    </label>
  );
  return (
    <fieldset className="sim__motion">
      <legend className="field__label">{t.t("How the phone is held")}</legend>
      <div className="sim__poses">
        {POSES.map((p) => (
          <button key={p.label} type="button" className="btn btn--ghost btn--sm" onClick={() => onChange({ ...motion, ...p.motion })}>
            {t.t(p.label)}
          </button>
        ))}
      </div>
      {slider("heading", "Facing (0 is north)", 0, 359)}
      {slider("beta", "Tilt forward and back", 0, 180)}
      {slider("gamma", "Tilt left and right", -90, 90)}
    </fieldset>
  );
}
