import { useState } from "react";
import type { Delivery } from "@zonventure/graph";
import { useT } from "../../i18n.tsx";
import { Card, errorText, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventPrintMaterial } from "./api.ts";

/**
 * How this event delivers its adventure's print materials. The adventure
 * decides what there is; the event may post a letter the adventure emails,
 * or the other way round, without a new version: an envelope emailed for
 * self-printing in summer and posted in winter. No materials, no card.
 */
export function PrintDeliveryCard({ eventId, materials, reload, invited }: { eventId: number; materials: EventPrintMaterial[]; reload: () => void; invited?: boolean }) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  if (materials.length === 0) return null;
  const label: Record<Delivery, string> = {
    posted: t.t("Posted by you"),
    emailed: t.t("Emailed to the buyer"),
    organiser: t.t("For organisers only"),
  };

  const set = async (m: EventPrintMaterial, d: Delivery) => {
    setBusy(true);
    try {
      await api.setPrintDelivery(eventId, { [m.id]: d === m.adventure_delivery ? "" : d });
      toast.ok(t.t("Saved"));
      reload();
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title={t.t("Print materials")} subtitle={invited ? t.t("How each of the adventure's print materials reaches each team of this event.") : t.t("How each of the adventure's print materials reaches a buyer of this event.")}>
      <ul className="ev-prints">
        {materials.map((m) => (
          <li key={m.id}>
            <span>{m.name || m.id}</span>
            <select value={m.delivery} disabled={busy} aria-label={t.t("Delivery of {{name}}", { name: m.name || m.id })}
              onChange={(e) => void set(m, e.target.value as Delivery)}>
              {(Object.keys(label) as Delivery[]).map((d) => (
                <option key={d} value={d}>{d === m.adventure_delivery ? t.t("{{delivery}} (as in the adventure)", { delivery: label[d] }) : label[d]}</option>
              ))}
            </select>
            {m.delivery !== m.adventure_delivery && <span className="ev-muted">{t.t("Changed for this event")}</span>}
          </li>
        ))}
      </ul>
    </Card>
  );
}
