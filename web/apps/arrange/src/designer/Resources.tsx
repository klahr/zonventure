import type { Counter, Graph, Item, JournalEntry, Zone } from "@zonventure/graph";
import { gridZones } from "@zonventure/graph";
import { useState } from "react";
import { useT } from "../i18n.tsx";
import { StaticMap } from "./StaticMap.tsx";
import { ZoneMap } from "./ZoneMap.tsx";
import { MediaEditor } from "./MediaEditor.tsx";

/** A copy without one key, for clearing an optional field under exactOptionalPropertyTypes. */
function withoutKey<T extends object, K extends keyof T>(obj: T, key: K): T {
  const next = { ...obj };
  delete next[key];
  return next;
}

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
  // A grid being laid out; only one map editor is open at a time.
  const [grid, setGrid] = useState<{ group: string; rows: number; cols: number; area: Zone } | null>(null);

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
        {[...new Set(zones.map((z) => z.group).filter((g): g is string => !!g))].map((g) => {
          const cells = zones.filter((z) => z.group === g);
          return (
            <details key={`group-${g}`} className="box">
              <summary>
                <strong>{g}</strong> · {t.plural(cells.length, { one: "{{n}} zone", other: "{{n}} zones" })}
              </summary>
              <div className="res__cells">
                {cells.map((z) => (
                  <input key={z.slug} className="field__input" value={z.name ?? ""} placeholder={z.slug} aria-label={t.t("Name")}
                    onChange={(e) => setZones(zones.map((x) => (x.slug === z.slug ? { ...z, name: e.target.value } : x)))} />
                ))}
              </div>
              <button type="button" className="link-btn" onClick={() => setZones(zones.filter((z) => z.group !== g))}>
                {t.t("Remove the whole group")}
              </button>
            </details>
          );
        })}
        {zones.map((z, i) => z.group ? null : (
          <div key={z.slug} className="box">
            <div className="box__row">
              <input className="field__input" value={z.name ?? ""} placeholder={z.slug} aria-label={t.t("Name")}
                onChange={(e) => setZones(zones.map((x, j) => (j === i ? { ...z, name: e.target.value } : x)))} />
              <input className="field__input res__section" value={z.group ?? ""} placeholder={t.t("Group")} aria-label={t.t("Group")} list="zone-groups"
                onChange={(e) => setZones(zones.map((x, j) => (j === i ? (e.target.value ? { ...z, group: e.target.value } : withoutKey(z, "group")) : x)))} />
              <button type="button" className="link-btn" onClick={() => setZones(zones.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
            </div>
            {open === z.slug ? (
              <ZoneMap zone={z} onChange={(nz) => setZones(zones.map((x, j) => (j === i ? nz : x)))} />
            ) : (
              <button type="button" className="zone-thumb" onClick={() => { setGrid(null); setOpen(z.slug); }} aria-label={t.t("Edit on the map")}>
                <StaticMap zone={z} width={320} height={120} />
                <span className="zone-thumb__edit">{t.t("Edit on the map")}</span>
              </button>
            )}
          </div>
        ))}
        {grid ? (
          <div className="box">
            <strong>{t.t("A grid of zones")}</strong>
            <p className="field__hint">{t.t("Place the four corners of the area, then choose how to cut it. Each cell becomes a zone — A1, B1, … — in one group, ready for a search.")}</p>
            <div className="box__row">
              <label className="field">
                <span className="field__label">{t.t("Group")}</span>
                <input className="field__input" value={grid.group} onChange={(e) => setGrid({ ...grid, group: e.target.value })} />
              </label>
              <label className="field field--num">
                <span className="field__label">{t.t("Rows")}</span>
                <input className="field__input" type="number" min={1} max={26} value={grid.rows} onChange={(e) => setGrid({ ...grid, rows: Math.max(1, Math.min(26, Number(e.target.value) || 1)) })} />
              </label>
              <label className="field field--num">
                <span className="field__label">{t.t("Columns")}</span>
                <input className="field__input" type="number" min={1} max={26} value={grid.cols} onChange={(e) => setGrid({ ...grid, cols: Math.max(1, Math.min(26, Number(e.target.value) || 1)) })} />
              </label>
            </div>
            <ZoneMap zone={grid.area} areaOnly maxCorners={4} onChange={(area) => setGrid({ ...grid, area })} />
            <div className="box__row">
              <button type="button" className="link-btn" disabled={(grid.area.polygon?.length ?? 0) !== 4 || !grid.group.trim()}
                onClick={() => {
                  setZones([...zones, ...gridZones(grid.area.polygon!, grid.rows, grid.cols, grid.group.trim(), zones)]);
                  setGrid(null);
                }}>
                {t.t("Create {{n}} zones", { n: grid.rows * grid.cols })}
              </button>
              <button type="button" className="link-btn" onClick={() => setGrid(null)}>{t.t("Cancel")}</button>
            </div>
          </div>
        ) : (
          <button type="button" className="link-btn"
            onClick={() => {
              const near = zones[zones.length - 1];
              setOpen(null);
              setGrid({ group: t.t("Grid"), rows: 7, cols: 6, area: { slug: "grid-area", lat: near?.lat ?? 59.3293, lng: near?.lng ?? 18.0686, radius_m: 0, polygon: [] } });
            }}>
            + {t.t("Add a grid of zones")}
          </button>
        )}
        <datalist id="zone-groups">
          {[...new Set(zones.map((z) => z.group).filter(Boolean))].map((g) => (
            <option key={g} value={g} />
          ))}
        </datalist>
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
        <p className="field__hint">{t.t("Things the team collects and can look at, for the story. To send teams different ways, a gate checks what they have done instead.")}</p>
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
        <h3>{t.t("Time limit")}</h3>
        <p className="field__hint">
          {t.t("After this long from a team's start, wherever it is, the team is moved to a step of your choosing — such as the accusation — and shown a message. It happens once.")}
        </p>
        <label className="check">
          <input type="checkbox" checked={!!graph.time_limit}
            onChange={(e) => onChange(e.target.checked
              ? { ...graph, time_limit: { seconds: 3 * 3600, to: graph.nodes.find((n) => n.type === "puzzle")?.slug ?? "" } }
              : withoutKey(graph, "time_limit"))} />
          {t.t("Limit the time")}
        </label>
        {graph.time_limit && (() => {
          const tl = graph.time_limit;
          const set = (next: typeof tl) => onChange({ ...graph, time_limit: next });
          return (
            <div className="box">
              <div className="box__row">
                <label className="field field--num">
                  <span className="field__label">{t.t("Minutes")}</span>
                  <input className="field__input" type="number" min={1} value={Math.round(tl.seconds / 60)}
                    onChange={(e) => set({ ...tl, seconds: Math.max(1, Number(e.target.value) || 1) * 60 })} />
                </label>
                <label className="field">
                  <span className="field__label">{t.t("Then send the team to")}</span>
                  <select className="field__input" value={tl.to} onChange={(e) => set({ ...tl, to: e.target.value })}>
                    <option value="">{t.t("— pick a step —")}</option>
                    {graph.nodes.filter((n) => n.type !== "start").map((n) => (
                      <option key={n.slug} value={n.slug}>{n.title || n.slug}</option>
                    ))}
                  </select>
                </label>
              </div>
              <textarea className="field__input" rows={3} value={tl.text ?? ""} placeholder={t.t("What the team reads when time is up. Markdown.")} aria-label={t.t("Message")}
                onChange={(e) => set(e.target.value ? { ...tl, text: e.target.value } : withoutKey(tl, "text"))} />
            </div>
          );
        })()}
      </section>

      <section className="insp__section">
        <h3>{t.t("Journal")}</h3>
        <p className="field__hint">
          {t.t("Notes the team collects and can read back at any time. A step adds them under “Notes for the journal”, or an effect does. Notes with the same section are listed together under it.")}
        </p>
        <label className="check">
          <input type="checkbox" checked={!!graph.journal_by_unlock}
            onChange={(e) => onChange(e.target.checked ? { ...graph, journal_by_unlock: true } : withoutKey(graph, "journal_by_unlock"))} />
          {t.t("List notes in the order the team found them, not the order written here")}
        </label>
        {journal.map((je, i) => {
          const set = (next: JournalEntry) => setJournal(journal.map((x, j) => (j === i ? next : x)));
          const move = (by: number) => {
            const to = i + by;
            if (to < 0 || to >= journal.length) return;
            const next = journal.slice();
            [next[i], next[to]] = [next[to]!, next[i]!];
            setJournal(next);
          };
          return (
            <div key={je.slug} className="box">
              <div className="box__row">
                <input className="field__input" value={je.title ?? ""} placeholder={je.slug} aria-label={t.t("Title")}
                  onChange={(e) => set({ ...je, title: e.target.value })} />
                <input className="field__input res__section" value={je.section ?? ""} placeholder={t.t("Section")} aria-label={t.t("Section")} list="journal-sections"
                  onChange={(e) => set(e.target.value ? { ...je, section: e.target.value } : withoutKey(je, "section"))} />
                <button type="button" className="link-btn" disabled={i === 0} onClick={() => move(-1)} aria-label={t.t("Move up")}>↑</button>
                <button type="button" className="link-btn" disabled={i === journal.length - 1} onClick={() => move(1)} aria-label={t.t("Move down")}>↓</button>
                <button type="button" className="link-btn" onClick={() => setJournal(journal.filter((_, j) => j !== i))}>{t.t("Remove")}</button>
              </div>
              <textarea className="field__input" rows={3} value={je.body ?? ""} aria-label={t.t("Text")} placeholder={t.t("Markdown.")}
                onChange={(e) => set({ ...je, body: e.target.value })} />
              <MediaEditor media={je.media} onChange={(media) => set(media ? { ...je, media } : withoutKey(je, "media"))} />
            </div>
          );
        })}
        <datalist id="journal-sections">
          {[...new Set(journal.map((je) => je.section).filter(Boolean))].map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <button type="button" className="link-btn" onClick={() => setJournal([...journal, { slug: slugFor("entry", journal), title: "" }])}>
          + {t.t("Add entry")}
        </button>
      </section>
    </div>
  );
}
