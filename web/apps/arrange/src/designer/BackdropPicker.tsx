import { useContext, useRef, useState, type CSSProperties, type PointerEvent } from "react";
import { adventures, type play } from "@zonventure/api";
import { BACKDROP_KINDS, BACKDROP_LIMITS, FULL_STRENGTH, backdropPalette, brandVars, isHex } from "@zonventure/play-ui";
import { ImagePlus, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { BACKDROP_PRESETS, presetOf, tidy, type BackdropEdit } from "./backdrops.ts";
import { Button, ICON } from "./dz/ui.tsx";
import { AdventureSlug, downscale } from "./MediaEditor.tsx";

/**
 * The background designer: pick a preset, then adjust it — colours,
 * strength, size, angle and a few switches. Each control shows only for
 * the patterns it changes. The phone preview above follows every change;
 * Save look stores the values, not the preset.
 */
export function BackdropPicker({ brand, onChange, media = true }: { brand: play.Brand; onChange: (b: play.Brand) => void; media?: boolean }) {
  const t = useT();
  const bd = brand.backdrop;
  const [base, setBase] = useState<string | null>(() => presetOf(bd));
  const matched = presetOf(bd);
  const preset = BACKDROP_PRESETS.find((p) => p.id === base);
  const caps = bd ? BACKDROP_KINDS[bd.kind] : null;
  const picture = bd?.kind === "picture";
  const slug = useContext(AdventureSlug);
  const file = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  // A picture keeps the strength, blur and switches it had when replaced.
  const usePicture = async (f: File) => {
    setUploading(true);
    setUploadError(null);
    try {
      const r = await adventures.uploadMedia(slug, await downscale(f, 2000));
      setBase(null);
      apply(picture && bd ? { ...bd, media: r.id } : { kind: "picture", media: r.id, opacity: 1, scale: 100, fixed: true });
    } catch (e) {
      setUploadError(e instanceof Error ? e.message : t.t("Could not upload the picture."));
    } finally {
      setUploading(false);
    }
  };
  const horizon = bd?.kind === "horizon";

  const apply = (next: BackdropEdit | undefined) => {
    const b = { ...brand };
    if (next) b.backdrop = tidy(next);
    else delete b.backdrop;
    onChange(b);
  };
  const choose = (id: string | null) => {
    setBase(id);
    apply(BACKDROP_PRESETS.find((p) => p.id === id)?.backdrop);
  };
  const edit = (patch: Partial<BackdropEdit>) => bd && apply({ ...bd, ...patch });

  // A thumbnail paints the preset over this adventure's own palette.
  const thumb = (b: play.Backdrop | undefined) => {
    const { backdrop: _, ...palette } = brand;
    return brandVars(b ? { ...palette, backdrop: b } : palette) as CSSProperties;
  };

  return (
    <div className="look__backdrop">
      <h4>{t.t("Background pattern")}</h4>
      <div className="look__patterns" role="radiogroup" aria-label={t.t("Background pattern")}>
        <button type="button" role="radio" aria-checked={!bd} className={"look__pattern" + (!bd ? " is-on" : "")} onClick={() => choose(null)}>
          <span className="look__thumb" style={thumb(undefined)} />
          <span>{t.t("None")}</span>
        </button>
        {media && (
          <button
            type="button"
            role="radio"
            aria-checked={picture}
            className={"look__pattern" + (picture ? " is-on" : "")}
            disabled={!slug || uploading}
            onClick={() => file.current?.click()}
          >
            {picture ? <span className="look__thumb" style={thumb(bd)} /> : <span className="look__thumb look__thumb--add"><ImagePlus size={ICON.md} aria-hidden /></span>}
            <span>{uploading ? t.t("Uploading…") : t.t("Picture")}</span>
          </button>
        )}
        {BACKDROP_PRESETS.map((p) => (
          <button key={p.id} type="button" role="radio" aria-checked={base === p.id && !!bd} className={"look__pattern" + (base === p.id && bd ? " is-on" : "")} onClick={() => choose(p.id)}>
            <span className="look__thumb" style={thumb(p.backdrop)} />
            <span>{t.t(p.label)}</span>
          </button>
        ))}
      </div>

      <input
        ref={file}
        type="file"
        accept="image/*"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (f) void usePicture(f);
        }}
      />
      {uploadError && <div className="alert alert--danger">{uploadError}</div>}
      {bd && caps && (
        <>
          <div className="look__status">
            <span className="field__hint">{matched ? t.t("Preset: {{name}}", { name: t.t(BACKDROP_PRESETS.find((p) => p.id === matched)!.label) }) : t.t("Custom")}</span>
            {preset && !matched && (
              <Button size="sm" onClick={() => choose(preset.id)}>
                {t.t("Reset to {{name}}", { name: t.t(preset.label) })}
              </Button>
            )}
          </div>
          {bd.kind === "spots" && <SpotCanvas brand={brand} bd={bd} onPoints={(points) => edit({ points })} />}
          {caps.colors && (
            <ColourList
              label={horizon ? t.t("Sky, from the top down to the horizon") : t.t("Colours in turn")}
              empty={horizon ? t.t("None: a purple night sky turning to the accent at the horizon.") : t.t("None: the pattern uses its one or two colours below.")}
              colours={bd.colors ?? []}
              onColours={(colors) => edit({ colors: colors.length ? colors : undefined })}
            />
          )}
          {caps.color && (horizon || !bd.colors?.length) && (
            <Colour
              label={horizon ? t.t("Grid colour") : bd.kind === "lined" ? t.t("Line colour") : t.t("Pattern colour")}
              hint={t.t("Empty follows the palette.")}
              value={bd.color}
              fallback={brand.text}
              onPick={(color) => edit({ color })}
            />
          )}
          {caps.color2 && (horizon || !bd.colors?.length) && (
            <Colour
              label={horizon ? t.t("Sun colour") : bd.kind === "lined" ? t.t("Margin colour") : t.t("Second colour")}
              hint={bd.kind === "stripes" ? t.t("Empty leaves every other stripe clear.") : horizon ? t.t("Empty is a warm gold.") : bd.kind === "lined" ? t.t("Empty is a soft red.") : t.t("Empty follows the palette.")}
              value={bd.color2}
              fallback={brand.accent_secondary ?? brand.accent}
              onPick={(color2) => edit({ color2 })}
            />
          )}
          <Slider
            label={t.t("Strength")}
            value={bd.opacity}
            min={BACKDROP_LIMITS.opacity[0]}
            max={FULL_STRENGTH.has(bd.kind) ? BACKDROP_LIMITS.spotsOpacity[1] : BACKDROP_LIMITS.opacity[1]}
            step={0.01}
            shown={`${Math.round(bd.opacity * 100)} %`}
            onSlide={(opacity) => edit({ opacity })}
          />
          {picture && (
            <>
              <Slider label={t.t("Blur")} value={bd.blur ?? 0} min={0} max={20} step={1} shown={`${bd.blur ?? 0} px`} onSlide={(blur) => edit({ blur: blur || undefined })} />
              <div className="look__actions">
                <Button size="sm" disabled={uploading} onClick={() => file.current?.click()}>
                  <ImagePlus size={ICON.sm} aria-hidden />
                  {t.t("Replace picture")}
                </Button>
              </div>
              <p className="field__hint">{t.t("Check that text reads over the picture: lower the strength, add blur, or frost the cards.")}</p>
            </>
          )}
          {!picture && <Slider label={bd.kind === "spots" ? t.t("Spread") : horizon ? t.t("Grid") : bd.kind === "lined" ? t.t("Line spacing") : t.t("Size")} value={bd.scale} min={BACKDROP_LIMITS.scale[0]} max={BACKDROP_LIMITS.scale[1]} step={1} shown={`${bd.scale} px`} onSlide={(scale) => edit({ scale })} />}
          {caps.angle && <Slider label={t.t("Angle")} value={bd.angle ?? 0} min={0} max={359} step={1} shown={`${bd.angle ?? 0}°`} onSlide={(angle) => edit({ angle })} />}
          <Check label={t.t("Fade toward the bottom")} checked={!!bd.fade} onCheck={(fade) => edit({ fade })} />
          <Check label={t.t("Stay still while the page scrolls")} checked={!!bd.fixed} onCheck={(fixed) => edit({ fixed })} />
          {caps.motion && <Check label={t.t("Drift slowly")} hint={t.t("Not on phones set to reduce motion.")} checked={!!bd.motion} onCheck={(motion) => edit({ motion })} />}
        </>
      )}
    </div>
  );
}

/**
 * Colour spots on a page the shape of a phone: click to add a spot, drag
 * one to move it, pick one to colour or remove it. Positions are percent
 * of the page, as the player places them.
 */
function SpotCanvas({ brand, bd, onPoints }: { brand: play.Brand; bd: play.Backdrop; onPoints: (p: play.BackdropSpot[]) => void }) {
  const t = useT();
  const box = useRef<HTMLDivElement>(null);
  const dragging = useRef<number | null>(null);
  const [picked, setPicked] = useState<number | null>(null);
  const points = bd.points ?? [];
  const spot = picked !== null ? points[picked] : undefined;
  // What a spot without a colour shows: the palette's colours in turn, as the player paints them.
  const palette = backdropPalette(brand);
  const shown = (p: play.BackdropSpot, i: number) => p.color ?? palette[i % palette.length]!;

  const at = (e: PointerEvent) => {
    const r = box.current!.getBoundingClientRect();
    const pct = (v: number) => Math.round(Math.min(100, Math.max(0, v)) * 10) / 10;
    return { x: pct(((e.clientX - r.left) / r.width) * 100), y: pct(((e.clientY - r.top) / r.height) * 100) };
  };
  const put = (i: number, p: Partial<play.BackdropSpot>) => onPoints(points.map((q, k) => (k === i ? { ...q, ...p } : q)));

  return (
    <div className="spots">
      <div
        ref={box}
        className="spots__page look__thumb"
        style={brandVars({ ...brand, backdrop: { ...bd, fixed: false } }) as CSSProperties}
        onPointerDown={(e) => {
          if (points.length >= BACKDROP_LIMITS.spots) return;
          onPoints([...points, at(e)]);
          setPicked(points.length);
        }}
        role="application"
        aria-label={t.t("Colour spots: click to add one, drag one to move it")}
      >
        {points.map((p, i) => (
          <button
            key={i}
            type="button"
            className={"spots__dot" + (i === picked ? " is-on" : "") + (p.color ? "" : " is-auto")}
            style={{ left: `${p.x}%`, top: `${p.y}%`, background: shown(p, i) }}
            aria-label={t.t("Spot {{n}}", { n: i + 1 })}
            onPointerDown={(e) => {
              e.stopPropagation();
              setPicked(i);
              dragging.current = i;
              e.currentTarget.setPointerCapture(e.pointerId);
            }}
            onPointerMove={(e) => dragging.current === i && put(i, at(e))}
            onPointerUp={() => (dragging.current = null)}
          />
        ))}
      </div>
      <p className="field__hint">{t.t("Click the page to add a spot ({{max}} at most), drag one to move it.", { max: BACKDROP_LIMITS.spots })}</p>
      {spot && picked !== null && (
        <div className="look__row look__row--short">
          <input type="color" className="look__swatch" aria-label={t.t("Spot colour")} value={shown(spot, picked)} onChange={(e) => put(picked, { color: e.target.value.toLowerCase() })} />
          <div className="look__text">
            <span className="look__label">{t.t("Spot {{n}}", { n: picked + 1 })}</span>
            <span className="field__hint">{spot.color ?? t.t("Follows the palette.")}</span>
          </div>
          {points.length > 1 && (
            <Button
              type="button"
              variant="ghost" size="icon" className="danger"
              aria-label={t.t("Remove this spot")}
              onClick={() => {
                onPoints(points.filter((_, k) => k !== picked));
                setPicked(null);
              }}
           >
              <X size={ICON.sm} />
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Colours in turn — rainbow stripes, party confetti. Empty, the pattern
 * uses its one or two colours instead.
 */
export function ColourList({ label, empty, colours, onColours }: { label: string; empty: string; colours: string[]; onColours: (c: string[]) => void }) {
  const t = useT();
  const RAINBOW = ["#ff595e", "#ffca3a", "#8ac926", "#1982c4", "#6a4c93"];
  return (
    <div className="field">
      <span className="field__label">{label}</span>
      <div className="colour-list">
        {colours.map((c, i) => (
          <span key={i} className="colour-list__item">
            <input type="color" className="look__swatch" aria-label={t.t("Colour {{n}}", { n: i + 1 })} value={c} onChange={(e) => onColours(colours.map((x, k) => (k === i ? e.target.value.toLowerCase() : x)))} />
            <Button variant="ghost" size="icon" className="danger" aria-label={t.t("Remove colour {{n}}", { n: i + 1 })} onClick={() => onColours(colours.filter((_, k) => k !== i))}>
              <X size={ICON.sm} />
            </Button>
          </span>
        ))}
        {colours.length < BACKDROP_LIMITS.colors && (
          <Button size="sm" onClick={() => onColours([...colours, RAINBOW[colours.length % RAINBOW.length]!])}>
            {t.t("Add colour")}
          </Button>
        )}
        {colours.length === 0 && (
          <Button size="sm" onClick={() => onColours(RAINBOW)}>
            {t.t("Rainbow")}
          </Button>
        )}
      </div>
      <span className="field__hint">{colours.length ? t.t("Used in turn, up to {{max}}.", { max: BACKDROP_LIMITS.colors }) : empty}</span>
    </div>
  );
}

function Colour(props: { label: string; hint: string; value: string | undefined; fallback: string | undefined; onPick: (v: string | undefined) => void }) {
  const t = useT();
  return (
    <div className="look__row look__row--short">
      <input
        type="color"
        className="look__swatch"
        aria-label={props.label}
        value={props.value ?? (isHex(props.fallback) ? props.fallback : "#1f3349")}
        onChange={(e) => props.onPick(e.target.value.toLowerCase())}
      />
      <div className="look__text">
        <span className="look__label">{props.label}</span>
        <span className="field__hint">{props.value ? props.value : props.hint}</span>
      </div>
      {props.value && (
        <Button variant="ghost" size="icon" className="danger" onClick={() => props.onPick(undefined)} aria-label={t.t("Use the default")}>
          <X size={ICON.sm} />
        </Button>
      )}
    </div>
  );
}

export function Slider(props: { label: string; value: number; min: number; max: number; step: number; shown: string; onSlide: (v: number) => void }) {
  return (
    <label className="field look__slider">
      <span className="field__label">
        {props.label} <span className="look__value">{props.shown}</span>
      </span>
      <input type="range" min={props.min} max={props.max} step={props.step} value={props.value} onChange={(e) => props.onSlide(Number(e.target.value))} />
    </label>
  );
}

export function Check(props: { label: string; hint?: string; checked: boolean; onCheck: (v: boolean) => void }) {
  return (
    <label className="look__check">
      <input type="checkbox" checked={props.checked} onChange={(e) => props.onCheck(e.target.checked)} />
      <span>
        {props.label}
        {props.hint && <span className="field__hint"> {props.hint}</span>}
      </span>
    </label>
  );
}
