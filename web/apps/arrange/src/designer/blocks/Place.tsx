import { MAP_ZOOM, type Graph, type Marker } from "@zonventure/graph";
import { CompassFace, FitMap } from "@zonventure/play-ui";
import { X } from "lucide-react";
import { ZoneSelect } from "../fields.tsx";
import { ICON } from "../dz/ui.tsx";
import { withField, type Editor, type Of, type T } from "./types.ts";

function PlaceSelect<B extends Of<"compass" | "map" | "radar">>({ block, set, t, graph }: { block: B; set: (b: B) => void; t: T; graph: Graph }) {
  return <ZoneSelect graph={graph} label={t.t("Zone")} empty={t.t("Choose a zone")} value={block.zone || undefined} onChange={(zone) => set({ ...block, zone: zone ?? "" })} />;
}

const zoneOf = (graph: Graph, slug: string) => graph.zones?.find((z) => z.slug === slug);

export const CompassEditor: Editor<"compass"> = ({ block, set, t, graph }) => {
  const zone = zoneOf(graph, block.zone);
  return (
    <>
      <PlaceSelect block={block} set={set} t={t} graph={graph} />
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

export const RadarEditor: Editor<"radar"> = ({ block, set, t, graph }) => (
  <>
    <PlaceSelect block={block} set={set} t={t} graph={graph} />
    <p className="field__hint">{t.t("Pulses faster and warmer as the team gets closer, without saying which way.")}</p>
  </>
);

export const MapEditor: Editor<"map"> = ({ block, set, t, graph }) => {
  const zone = zoneOf(graph, block.zone);
  const markers = block.markers ?? [];
  const setMarkers = (next: Marker[]) => set(withField(block, "markers", next.length ? next : undefined));
  return (
    <>
      <PlaceSelect block={block} set={set} t={t} graph={graph} />
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
