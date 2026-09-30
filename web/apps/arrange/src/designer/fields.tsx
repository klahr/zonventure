import { useState, type ReactNode } from "react";
import { isPolygon, type Graph, type Zone } from "@zonventure/graph";
import { LocateFixed, MapPin } from "lucide-react";
import { useT } from "../i18n.tsx";
import { ICON } from "./dz/ui.tsx";
import { locate, zoneAt } from "./zones.ts";

export function Field({ label, hint, children }: { label: string; hint?: string | undefined; children: ReactNode }) {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      {children}
      {hint && <span className="field__hint">{hint}</span>}
    </label>
  );
}

/**
 * Lets a zone-picking field create its zone where the author stands: the
 * caller writes the new zone list and the choice in one edit.
 */
export interface ZoneHere {
  /** What a new zone is called: the step's name. */
  name: string;
  set: (zones: Zone[], slug: string) => void;
}

const SIZES = [15, 30, 60];

export function ZoneSelect({ graph, label, value, onChange, empty, here }: { graph: Graph; label: string; value: string | undefined; onChange: (z: string | undefined) => void; empty?: string; here?: ZoneHere }) {
  const t = useT();
  const zones = graph.zones ?? [];
  const chosen = zones.find((z) => z.slug === value);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);

  // Pin a new zone, or move the chosen one, to where the phone is.
  const pin = async (move: boolean) => {
    if (!here) return;
    setBusy(true);
    setNote(null);
    try {
      const p = await locate(t);
      if (move && chosen) here.set(zones.map((z) => (z.slug === chosen.slug ? { ...z, lat: p.lat, lng: p.lng } : z)), chosen.slug);
      else {
        const z = zoneAt(zones, here.name || t.t("Place"), p);
        here.set([...zones, z], z.slug);
      }
      setNote(
        p.accuracy > 50
          ? { text: t.t("GPS is rough here (about {{m}} m). Wait a moment, then tap Move it here again.", { m: p.accuracy }), warn: true }
          : { text: t.t("Pinned to within about {{m}} m.", { m: p.accuracy }) },
      );
    } catch (e) {
      setNote({ text: e instanceof Error ? e.message : String(e), warn: true });
    } finally {
      setBusy(false);
    }
  };

  const hint = here ? undefined : zones.length === 0 ? t.t("Add zones under Adventure settings (⋮ menu) → Zones.") : undefined;
  return (
    <>
      <Field label={label} hint={hint}>
        <select className="field__input" value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
          <option value="">{empty ?? t.t("Anywhere")}</option>
          {zones.map((z) => (
            <option key={z.slug} value={z.slug}>
              {z.name || z.slug} ({isPolygon(z) ? t.t("area") : `${z.radius_m} m`})
            </option>
          ))}
        </select>
      </Field>
      {here && (
        <div className="zone-here">
          <div className="zone-here__row">
            <button type="button" className="add-btn concept--place" disabled={busy} onClick={() => void pin(false)}>
              <MapPin size={ICON.sm} aria-hidden="true" />
              {busy ? t.t("Finding you…") : t.t("Mark where I stand")}
            </button>
            {chosen && !isPolygon(chosen) && (
              <button type="button" className="link-btn" disabled={busy} onClick={() => void pin(true)}>
                <LocateFixed size={ICON.sm} aria-hidden="true" />
                {t.t("Move it here")}
              </button>
            )}
          </div>
          {chosen && !isPolygon(chosen) && (
            <div className="seg zone-here__size" role="group" aria-label={t.t("Size")}>
              {[...new Set([...SIZES, chosen.radius_m])].sort((a, b) => a - b).map((m) => (
                <button key={m} type="button" className={m === chosen.radius_m ? "is-on" : ""} onClick={() => here.set(zones.map((z) => (z.slug === chosen.slug ? { ...z, radius_m: m } : z)), chosen.slug)}>
                  {m} m
                </button>
              ))}
            </div>
          )}
          {note && <span className={"field__hint" + (note.warn ? " zone-here__warn" : "")}>{note.text}</span>}
          {!chosen && !note && <span className="field__hint">{t.t("Stand at the spot and tap Mark where I stand. The team must be within the circle to answer.")}</span>}
        </div>
      )}
    </>
  );
}
