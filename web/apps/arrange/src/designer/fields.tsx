import type { ReactNode } from "react";
import { isPolygon, type Graph } from "@zonventure/graph";
import { useT } from "../i18n.tsx";

export function Field({ label, hint, children }: { label: string; hint?: string | undefined; children: ReactNode }) {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      {children}
      {hint && <span className="field__hint">{hint}</span>}
    </label>
  );
}

export function ZoneSelect({ graph, label, value, onChange, empty }: { graph: Graph; label: string; value: string | undefined; onChange: (z: string | undefined) => void; empty?: string }) {
  const t = useT();
  const zones = graph.zones ?? [];
  return (
    <Field label={label} hint={zones.length === 0 ? t.t("Add zones under Adventure settings (⋮ menu) → Zones.") : undefined}>
      <select className="field__input" value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
        <option value="">{empty ?? t.t("Anywhere")}</option>
        {zones.map((z) => (
          <option key={z.slug} value={z.slug}>
            {z.name || z.slug} ({isPolygon(z) ? t.t("area") : `${z.radius_m} m`})
          </option>
        ))}
      </select>
    </Field>
  );
}
