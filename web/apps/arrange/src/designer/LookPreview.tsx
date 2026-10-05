import { useState } from "react";
import type { play } from "@zonventure/api";
import type { Envelope, Graph } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { VISIBLE } from "./dz/types.tsx";
import { Simulator } from "./Simulator.tsx";
import { replayAnimations } from "./blocks/Animation.tsx";

const nowhere = () => {};

/**
 * The look as a team sees it, beside the Look controls: one of the
 * draft's steps on a small phone, in the palette, fonts and backdrop
 * being edited — before they are saved. It is the simulator's own phone,
 * so it shows exactly what the player will.
 */
export function LookPreview({ slug, graph, envelope, brand, title, selected }: { slug: string; graph: Graph; envelope: Envelope; brand: play.Brand; title: string; selected: string | null }) {
  const t = useT();
  const steps = graph.nodes.filter((n) => VISIBLE.has(n.type));
  const [step, setStep] = useState<string | null>(() => (selected && steps.some((n) => n.slug === selected) ? selected : (steps.find((n) => n.type === "puzzle") ?? steps[0])?.slug ?? null));
  if (!steps.length) return <p className="field__hint">{t.t("Add a puzzle or a story to preview the look on it.")}</p>;
  return (
    <div className="look-preview">
      <label className="field">
        <span className="field__label">{t.t("Preview on")}</span>
        <select className="field__input" value={step ?? ""} onChange={(e) => setStep(e.target.value)}>
          {steps.map((n) => (
            <option key={n.slug} value={n.slug}>
              {n.title || n.slug}
            </option>
          ))}
        </select>
      </label>
      <button type="button" className="btn btn--ghost btn--sm" onClick={replayAnimations} title={t.t("Show this step again, animations and all")}>
        {t.t("Show again")}
      </button>
      <Simulator bare slug={slug} graph={graph} envelope={envelope} selected={step} mode="step" restart={0} onPosition={nowhere} brand={brand} title={title} />
    </div>
  );
}
