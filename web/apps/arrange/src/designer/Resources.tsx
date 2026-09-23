import type { Counter, Graph, Item, JournalEntry, Zone } from "@zonventure/graph";
import { useState } from "react";
import { useT } from "../i18n.tsx";
import { StaticMap } from "./StaticMap.tsx";
import { ZoneMap } from "./ZoneMap.tsx";

/**
 * Adventure-level resources nodes refer to by slug: zones, items, shared
 * counters and journal entries.
 *
 * They live beside the canvas rather than on it. A geofence drawn as a
 * node would show the world instead of the flow, and the flow is what an
 * author is reasoning about on the canvas.
 *
 * Zones are edited on a map, one at a time: each open editor is a WebGL
 * context, and browsers cap those per page. The rest show a static map.
 */
export function Resources({ graph, onChange }: { graph: Graph; onChange: (g: Graph) => void }) {
  const t = useT();
  const [open, setOpen] = useState<string | null>(null);

  const slugFor = (prefix: string, taken: { slug: string }[]) => {
    const used = new Set(taken.map((x) => x.slug));
    for (let i = 1; ; i++) if (!used.has(`${prefix}-${i}`)) return `${prefix}-${i}`;
  };

  const zones = graph.zones ?? [];
  const items = graph.items ?? [];
  const counters = graph.counters ?? [];
  const journal = graph.journal ?? [];

  const setZones = (z: Zone[]) => onChange({ ...graph, zones: z });
  const setItems = (i: Item[]) => onChange({ ...graph, items: i });
  const setCounters = (c: Counter[]) => onChange({ ...graph, counters: c });
  const setJournal = (j: JournalEntry[]) => onChange({ ...graph, journal: j });

  return (
    <div className="res">
      <section className="insp__section">
        <h3>{t.t("Zones")}</h3>
        <p className="field__hint">{t.t("Places on the map. Puzzles, stories and conditions can require the team to be inside one.")}</p>
        {zones.map((z, i) => (
          <div key={z.slug} className="box">
            <div className="box__row">
              <input className="field__input" value={z.name ?? ""} placeholder={z.slug} aria-label={t.t("Name")}
                onChange={(e) => setZones(zones.map((x, j) => (j === i ? { ...z, name: e.target.value } : x)))} />
              <button type="button" className="link-btn" onClick={() => setZones(zones.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
            </div>
            {open === z.slug ? (
              <ZoneMap zone={z} onChange={(nz) => setZones(zones.map((x, j) => (j === i ? nz : x)))} />
            ) : (
              <button type="button" className="zone-thumb" onClick={() => setOpen(z.slug)} aria-label={t.t("Edit on the map")}>
                <StaticMap zone={z} width={320} height={120} />
                <span className="zone-thumb__edit">{t.t("Edit on the map")}</span>
              </button>
            )}
          </div>
        ))}
        <button type="button" className="link-btn"
          onClick={() => {
            const slug = slugFor("zone", zones);
            const near = zones[zones.length - 1];
            setZones([...zones, { slug, name: "", lat: near?.lat ?? 59.3293, lng: near?.lng ?? 18.0686, radius_m: 30 }]);
            setOpen(slug);
          }}>
          + {t.t("Add zone")}
        </button>
      </section>

      <section className="insp__section">
        <h3>{t.t("Items")}</h3>
        <p className="field__hint">{t.t("Things the team collects and can see. No gate needs them — history does that — they are for the story.")}</p>
        {items.map((it, i) => (
          <div key={it.slug} className="box__row">
            <input className="field__input" value={it.name ?? ""} placeholder={it.slug} aria-label={t.t("Name")}
              onChange={(e) => setItems(items.map((x, j) => (j === i ? { ...it, name: e.target.value } : x)))} />
            <button type="button" className="link-btn" onClick={() => setItems(items.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
          </div>
        ))}
        <button type="button" className="link-btn" onClick={() => setItems([...items, { slug: slugFor("item", items), name: "" }])}>
          + {t.t("Add item")}
        </button>
      </section>

      <section className="insp__section">
        <h3>{t.t("Shared counters")}</h3>
        <p className="field__hint">{t.t("One number every team in the event sees and changes.")}</p>
        {counters.map((c, i) => (
          <div key={c.slug} className="box">
            <div className="box__row">
              <input className="field__input" value={c.name ?? ""} placeholder={c.slug} aria-label={t.t("Name")}
                onChange={(e) => setCounters(counters.map((x, j) => (j === i ? { ...c, name: e.target.value } : x)))} />
              <button type="button" className="link-btn" onClick={() => setCounters(counters.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
            </div>
            <div className="box__row">
              {(["initial", "min", "max"] as const).map((k) => (
                <label key={k} className="field field--num"><span className="field__label">{t.t(k)}</span>
                  <input className="field__input" type="number" value={c[k] ?? ""}
                    onChange={(e) => setCounters(counters.map((x, j) => {
                      if (j !== i) return x;
                      const next = { ...c };
                      if (e.target.value === "") delete next[k];
                      else next[k] = Number(e.target.value);
                      return next;
                    }))} /></label>
              ))}
            </div>
          </div>
        ))}
        <button type="button" className="link-btn" onClick={() => setCounters([...counters, { slug: slugFor("counter", counters), name: "" }])}>
          + {t.t("Add counter")}
        </button>
      </section>

      <section className="insp__section">
        <h3>{t.t("Journal")}</h3>
        <p className="field__hint">{t.t("Lore an effect unlocks, which the team can read back later.")}</p>
        {journal.map((je, i) => (
          <div key={je.slug} className="box">
            <div className="box__row">
              <input className="field__input" value={je.title ?? ""} placeholder={je.slug} aria-label={t.t("Title")}
                onChange={(e) => setJournal(journal.map((x, j) => (j === i ? { ...je, title: e.target.value } : x)))} />
              <button type="button" className="link-btn" onClick={() => setJournal(journal.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
            </div>
            <textarea className="field__input" rows={3} value={je.body ?? ""} aria-label={t.t("Text")}
              onChange={(e) => setJournal(journal.map((x, j) => (j === i ? { ...je, body: e.target.value } : x)))} />
          </div>
        ))}
        <button type="button" className="link-btn" onClick={() => setJournal([...journal, { slug: slugFor("entry", journal), title: "" }])}>
          + {t.t("Add entry")}
        </button>
      </section>
    </div>
  );
}
