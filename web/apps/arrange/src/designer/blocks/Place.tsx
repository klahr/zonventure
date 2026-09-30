import { useState } from "react";
import { MAP_ZOOM, type Graph, type Marker, type Zone } from "@zonventure/graph";
import { CompassFace, FitMap } from "@zonventure/play-ui";
import { X } from "lucide-react";
import { Field, ZoneSelect } from "../fields.tsx";
import { ZoneMap } from "../ZoneMap.tsx";
import { nearLastZone, zoneAt } from "../zones.ts";
import { ICON } from "../dz/ui.tsx";
import { withField, type Editor, type Of, type T } from "./types.ts";

function PlaceSelect<B extends Of<"compass" | "map" | "radar">>({ block, set, t, graph, withZones }: { block: B; set: (b: B) => void; t: T; graph: Graph; withZones?: ((zones: Zone[], b: B) => void) | undefined }) {
  const [editing, setEditing] = useState(false);
  const zones = graph.zones ?? [];
  // A compass or map usually points on to the next place, not this
  // step's own: a new one gets a plain name that is its own, to rename.
  const taken = new Set(zones.map((z) => z.name));
  let name = t.t("New place");
  for (let i = 2; taken.has(name); i++) name = `${t.t("New place")} ${i}`;
  const zone = zoneOf(graph, block.zone);
  const select = (
    <ZoneSelect
      graph={graph}
      label={t.t("Zone")}
      empty={t.t("Choose a zone")}
      value={block.zone || undefined}
      onChange={(slug) => set({ ...block, zone: slug ?? "" })}
      {...(withZones
        ? {
            // A new place starts beside the last one; its map opens so
            // the pin can be dragged, or put where the author stands.
            onNew: () => {
              const z = zoneAt(zones, name, nearLastZone(zones));
              withZones([...zones, z], { ...block, zone: z.slug });
              setEditing(true);
            },
          }
        : {})}
    />
  );
  if (!withZones || !zone) return select;
  const editZone = (z: Zone) => withZones(zones.map((x) => (x.slug === z.slug ? z : x)), block);
  return (
    <>
      {select}
      <button type="button" className="link-btn blk__zone-edit" aria-expanded={editing} onClick={() => setEditing((v) => !v)}>
        {editing ? t.t("Done") : t.t("Edit the place on the map")}
      </button>
      {editing && (
        <div className="blk__zone">
          <Field label={t.t("Name of the place")} hint={t.t("The team sees this name.")}>
            <input className="field__input" value={zone.name ?? ""} placeholder={zone.slug} onChange={(e) => editZone({ ...zone, name: e.target.value })} />
          </Field>
          <ZoneMap key={zone.slug} zone={zone} onChange={editZone} />
        </div>
      )}
    </>
  );
}

const zoneOf = (graph: Graph, slug: string) => graph.zones?.find((z) => z.slug === slug);

export const CompassEditor: Editor<"compass"> = ({ block, set, t, graph, withZones }) => {
  const zone = zoneOf(graph, block.zone);
  return (
    <>
      <PlaceSelect block={block} set={set} t={t} graph={graph} withZones={withZones} />
      <label className="check">
        <input type="checkbox" checked={!block.hide_distance} onChange={(e) => set(withField(block, "hide_distance", !e.target.checked))} />
        <span>{t.t("Show the distance")}</span>
      </label>
      {zone && (
        <div className="blk__preview" aria-label={t.t("Preview")}>
          <CompassFace name={zone.name || zone.slug} angle={45} northUp={false} t={(k, v) => t.t(k, v)} distance={block.hide_distance ? null : t.t("About {{m}} m away", { m: 250 })} />
        </div>
      )}
      <p className="field__hint">{t.t("Points the phone at the zone. Without a compass on the phone, north is up.")}</p>
    </>
  );
};

export const RadarEditor: Editor<"radar"> = ({ block, set, t, graph, withZones }) => (
  <>
    <PlaceSelect block={block} set={set} t={t} graph={graph} withZones={withZones} />
    <p className="field__hint">{t.t("Pulses faster and warmer as the team gets closer, without saying which way.")}</p>
  </>
);

export const MapEditor: Editor<"map"> = ({ block, set, t, graph, withZones }) => {
  const zone = zoneOf(graph, block.zone);
  const markers = block.markers ?? [];
  const setMarkers = (next: Marker[]) => set(withField(block, "markers", next.length ? next : undefined));
  return (
    <>
      <PlaceSelect block={block} set={set} t={t} graph={graph} withZones={withZones} />
      <label className="check">
        <input type="checkbox" checked={!block.hide_team} onChange={(e) => set(withField(block, "hide_team", !e.target.checked))} />
        <span>{t.t("Show the team's own position")}</span>
      </label>
      <label className="check">
        <input
          type="checkbox"
          checked={!block.zoom}
          onChange={(e) => {
            const { zoom: _, ...rest } = block;
            set(e.target.checked ? rest : { ...rest, zoom: 16 });
          }}
        />
        <span>{t.t("Fit the map to the zone and markers")}</span>
      </label>
      {block.zoom !== undefined && (
        <label className="field blk__zoom">
          <span className="field__label">{t.t("Zoom: {{n}}", { n: block.zoom })}</span>
          <input type="range" min={MAP_ZOOM.min} max={MAP_ZOOM.max} value={block.zoom} onChange={(e) => set({ ...block, zoom: Number(e.target.value) })} />
          <span className="blk__zoom-ends" aria-hidden="true">
            <span>{t.t("Wider")}</span>
            <span>{t.t("Closer")}</span>
          </span>
        </label>
      )}
      {zone && <FitMap className="blk__preview" zone={zone} zoom={block.zoom} pins={markers} onPick={(p) => setMarkers([...markers, { lat: round(p.lat), lng: round(p.lng) }])} />}
      {zone && <p className="field__hint">{t.t("Click the map to add a marker there.")}</p>}
      {markers.map((m, i) => (
        <div key={i} className="blk__marker">
          <input
            className="field__input"
            value={m.label ?? ""}
            placeholder={t.t("Marker {{n}}", { n: i + 1 })}
            aria-label={t.t("Marker label")}
            onChange={(e) => setMarkers(markers.map((x, j) => (j === i ? withField(x, "label", e.target.value) : x)))}
          />
          <span className="blk__coords">
            {m.lat.toFixed(5)}, {m.lng.toFixed(5)}
          </span>
          <button type="button" className="icon-remove" onClick={() => setMarkers(markers.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <p className="field__hint">{block.hide_team ? t.t("The team does not see itself on this map.") : block.zoom ? t.t("The team sees itself on the map whenever it is inside the frame.") : t.t("The team sees itself on the map when it is within 3 km.")}</p>
    </>
  );
};

/** Six decimals is 10 cm: plenty for a pin, and a stable diff. */
const round = (n: number) => Math.round(n * 1e6) / 1e6;
