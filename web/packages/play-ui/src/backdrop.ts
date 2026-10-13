import type { play } from "@zonventure/api";

/**
 * A backdrop — a pattern behind the cards — painted from its kind and
 * settings (internal/adventure: Backdrop). The author never writes CSS:
 * every value here is a number clamped to its range or a colour checked
 * to be #rrggbb, so nothing they type reaches a style but these.
 *
 * The result is custom properties the .pv rule in play-ui.css reads:
 * one list of background layers, with their sizes, positions, repeats
 * and attachment, plus a drift animation's end positions. Text sits on
 * the cards, which stay solid, so a pattern never sits under it.
 */

const HEX = /^#[0-9a-f]{6}$/i;
const DEFAULT_INK = "#1f3349";
const DEFAULT_ACCENT = "#2f6fb3";

/** What each kind can use, so the designer shows only the controls that do something. */
export const BACKDROP_KINDS: Record<play.BackdropKind, { color: boolean; angle: boolean; color2: boolean; motion: boolean; colors: boolean }> = {
  dots: { color: true, angle: false, color2: false, motion: true, colors: false },
  stripes: { color: true, angle: true, color2: true, motion: false, colors: true },
  checks: { color: true, angle: false, color2: false, motion: true, colors: false },
  grid: { color: true, angle: false, color2: false, motion: true, colors: false },
  sunburst: { color: true, angle: true, color2: false, motion: false, colors: false },
  waves: { color: true, angle: false, color2: false, motion: true, colors: false },
  zigzag: { color: true, angle: false, color2: false, motion: true, colors: false },
  confetti: { color: true, angle: false, color2: true, motion: true, colors: true },
  snow: { color: true, angle: false, color2: true, motion: true, colors: true },
  gradient: { color: true, angle: true, color2: true, motion: false, colors: false },
  // Each spot carries its own colour.
  spots: { color: false, angle: false, color2: false, motion: false, colors: false },
  // A retro scene: the grid's colour, the sun's, and the sky's in turn.
  horizon: { color: true, angle: false, color2: true, motion: false, colors: true },
  // A notebook page: the rules' colour and the margin's.
  lined: { color: true, angle: false, color2: true, motion: false, colors: false },
  // An uploaded image, on a layer of its own so it can be blurred (PlayView).
  picture: { color: false, angle: false, color2: false, motion: false, colors: false },
};

/** Kinds that are the background itself rather than a pattern on it, and so may go to full strength. */
export const FULL_STRENGTH: ReadonlySet<string> = new Set(["spots", "horizon", "picture"]);

/** internal/graph/look's ranges. Colour spots are the background itself, so they may go to full colour. */
export const BACKDROP_LIMITS = { opacity: [0.02, 0.6], spotsOpacity: [0.02, 1], scale: [8, 200], spots: 12, colors: 8 } as const;

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, Number.isFinite(v) ? v : lo));
const px = (n: number) => `${Math.round(n * 100) / 100}px`;

function rgba(hex: string, a: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${Math.round(a * 1000) / 1000})`;
}

/** a moved toward b by t (0–1), as #rrggbb. */
function mix(a: string, b: string, t: number): string {
  const ca = parseInt(a.slice(1), 16);
  const cb = parseInt(b.slice(1), 16);
  const ch = (n: number, s: number) => (n >> s) & 255;
  return "#" + [16, 8, 0].map((s) => Math.round(ch(ca, s) + (ch(cb, s) - ch(ca, s)) * t).toString(16).padStart(2, "0")).join("");
}

/**
 * A retro horizon, as the 1980s drew it: a sky from the colour list
 * (top to horizon) with stars, a striped sun, a glow on the horizon and a
 * grid floor running to it in perspective. Drawn in a portrait box and
 * sliced to cover the page, so it never stretches.
 */
function horizonSVG(b: play.Backdrop, grid: string, sun: string, palette: string[], o: number): string {
  const W = 400;
  const Hh = 800;
  const H = 470; // the horizon
  const list = colourList(b);
  const sky = list.length >= 2 ? list : ["#1c0e31", "#2e1f56", "#59255b", palette[0]!];
  const low = sky[sky.length - 1]!;
  const stops = sky.map((c, i) => `<stop offset="${(i / (sky.length - 1)).toFixed(3)}" stop-color="${c}"/>`).join("");
  const rnd = seeded(80);
  let stars = "";
  for (let i = 0; i < 70; i++) {
    stars += `<circle cx="${(rnd() * W).toFixed(1)}" cy="${(rnd() * H * 0.8).toFixed(1)}" r="${(0.5 + rnd() * 1.2).toFixed(2)}" fill="#ffffff" opacity="${(0.3 + rnd() * 0.6).toFixed(2)}"/>`;
  }
  // The sun's lower half is cut by bands that thicken toward the horizon.
  let bands = `<rect x="0" y="0" width="${W}" height="${H - 58}"/>`;
  for (let k = 0, y = H - 58; y < H; k++) {
    const gap = 2 + k * 1.6;
    const solid = Math.max(3, 10 - k * 1.5);
    bands += `<rect x="0" y="${y.toFixed(1)}" width="${W}" height="${solid.toFixed(1)}"/>`;
    y += solid + gap;
  }
  const step = Math.max(14, Math.min(200, b.scale)) * 2.2; // grid spacing at the bottom edge
  let lines = "";
  for (let k = -Math.ceil(W / step) * 3; k <= Math.ceil(W / step) * 3; k++) {
    lines += `<line x1="${W / 2}" y1="${H}" x2="${(W / 2 + k * step).toFixed(1)}" y2="${Hh}"/>`;
  }
  const rows = Math.round(Math.max(6, Math.min(24, 600 / step + 6)));
  for (let j = 1; j <= rows; j++) {
    const y = H + (Hh - H) * Math.pow(j / rows, 2.1);
    lines += `<line x1="0" y1="${y.toFixed(1)}" x2="${W}" y2="${y.toFixed(1)}"/>`;
  }
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${Hh}" preserveAspectRatio="xMidYMid slice">` +
    `<defs><linearGradient id="s" x1="0" y1="0" x2="0" y2="1">${stops}</linearGradient>` +
    `<linearGradient id="u" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${sun}"/><stop offset="1" stop-color="${low}"/></linearGradient>` +
    `<linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${low}" stop-opacity="0"/><stop offset="1" stop-color="${low}" stop-opacity="0.9"/></linearGradient>` +
    `<linearGradient id="f" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${low}" stop-opacity="0.55"/><stop offset="0.35" stop-color="${sky[0]}"/></linearGradient>` +
    `<clipPath id="c">${bands}</clipPath></defs>` +
    `<g opacity="${o}">` +
    `<rect width="${W}" height="${H}" fill="url(#s)"/>${stars}` +
    `<circle cx="${W / 2}" cy="${H - 30}" r="100" fill="url(#u)" clip-path="url(#c)"/>` +
    `<rect y="${H - 90}" width="${W}" height="90" fill="url(#g)"/>` +
    `<rect y="${H}" width="${W}" height="${Hh - H}" fill="url(#f)"/>` +
    `<g stroke="${grid}" stroke-width="1.3" stroke-opacity="0.85">${lines}</g>` +
    `<line x1="0" y1="${H}" x2="${W}" y2="${H}" stroke="${grid}" stroke-width="2"/>` +
    `</g></svg>`
  );
}

/**
 * A picture backdrop. The player paints it on a layer of its own
 * (.pv-backdrop-photo, PlayView), since CSS cannot blur a background image; the
 * same image as a plain background serves a thumbnail, which has no
 * such layer and is never blurred. The URL is play.mediaURL's.
 */
export function pictureVars(b: play.Backdrop): Record<string, string> {
  const id = Math.trunc(Number(b.media));
  if (!(id > 0)) return {};
  const url = `url("/media/${id}")`;
  const blur = clamp(b.blur ?? 0, 0, 20);
  return {
    "--pv-photo": url,
    "--pv-photo-opacity": String(clamp(b.opacity, ...BACKDROP_LIMITS.spotsOpacity)),
    "--pv-photo-filter": blur > 0 ? `blur(${blur}px)` : "none",
    "--pv-photo-position": b.fixed ? "fixed" : "absolute",
    "--pv-photo-mask": b.fade ? "linear-gradient(to bottom, #000 35%, transparent 95%)" : "none",
    "--pv-backdrop": url,
    "--pv-backdrop-size": "cover",
    "--pv-backdrop-pos": "50% 50%",
    "--pv-backdrop-repeat": "no-repeat",
    "--pv-backdrop-attach": "scroll",
  };
}

/** Whether a palette's backdrop is a picture the player paints on its own layer. */
export const hasPicture = (b: play.Brand | null | undefined) => b?.backdrop?.kind === "picture" && Number(b.backdrop.media) > 0;

/** A backdrop's colour list, its valid #rrggbb entries only. */
const colourList = (b: play.Backdrop) => (b.colors ?? []).filter((c) => HEX.test(c)).slice(0, BACKDROP_LIMITS.colors);

const svgURL = (svg: string) => `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;

/** A small seeded generator, so confetti lands the same way on every phone. */
function seeded(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

interface Layer {
  image: string;
  size: string;
  pos: string;
  /** Where the drift ends: the start moved by one whole tile, so the loop is seamless. */
  to: string;
  repeat: string;
}

const tile = (image: string, w: number, h: number, x = 0, y = 0): Layer => ({
  image,
  size: `${px(w)} ${px(h)}`,
  pos: `${px(x)} ${px(y)}`,
  to: `${px(x + w)} ${px(y + h)}`,
  repeat: "repeat",
});

const whole = (image: string): Layer => ({ image, size: "100% 100%", pos: "0 0", to: "0 0", repeat: "no-repeat" });

function layers(b: play.Backdrop, ink: string, second: string, palette: string[]): Layer[] {
  const s = clamp(b.scale, ...BACKDROP_LIMITS.scale);
  const [lo, hi] = FULL_STRENGTH.has(b.kind) ? BACKDROP_LIMITS.spotsOpacity : BACKDROP_LIMITS.opacity;
  const o = clamp(b.opacity, lo, hi);
  const a = clamp(b.angle ?? 0, 0, 359);
  const c = rgba(ink, o);
  switch (b.kind) {
    case "dots": {
      const r = s * 0.14;
      const dot = `radial-gradient(circle, ${c} ${px(r)}, transparent ${px(r + 0.6)})`;
      return [tile(dot, s, s), tile(dot, s, s, s / 2, s / 2)];
    }
    case "stripes": {
      // A list of colours makes bands in turn — a rainbow.
      const list = colourList(b);
      if (list.length) {
        const w = s / 2;
        const bands = list.map((c, i) => `${rgba(c, o)} ${px(i * w)} ${px((i + 1) * w)}`).join(", ");
        return [{ image: `repeating-linear-gradient(${a}deg, ${bands})`, size: "auto", pos: "0 0", to: "0 0", repeat: "repeat" }];
      }
      const other = b.color2 && HEX.test(b.color2) ? rgba(b.color2, o) : "transparent";
      return [{ image: `repeating-linear-gradient(${a}deg, ${c} 0 ${px(s / 2)}, ${other} ${px(s / 2)} ${px(s)})`, size: "auto", pos: "0 0", to: "0 0", repeat: "repeat" }];
    }
    case "checks":
      return [tile(`linear-gradient(90deg, ${c} 50%, transparent 50%)`, s, s), tile(`linear-gradient(0deg, ${c} 50%, transparent 50%)`, s, s)];
    case "grid": {
      const w = Math.max(1, s / 24);
      return [tile(`linear-gradient(${c} ${px(w)}, transparent ${px(w)})`, s, s), tile(`linear-gradient(90deg, ${c} ${px(w)}, transparent ${px(w)})`, s, s)];
    }
    case "sunburst": {
      const ray = clamp(s / 6, 2, 30);
      return [whole(`repeating-conic-gradient(from ${a}deg at 50% 22%, ${c} 0deg ${ray}deg, transparent ${ray}deg ${ray * 2}deg)`)];
    }
    case "gradient":
      return [whole(`linear-gradient(${a}deg, ${rgba(ink, o)}, ${rgba(second, o)})`)];
    case "lined": {
      // Rules every Scale pixels, and a margin line a little in from the left.
      const w = Math.max(1, s / 28);
      const margin = b.color2 && HEX.test(b.color2) ? b.color2 : "#e57373";
      const m = rgba(margin, Math.min(1, o * 2.5));
      return [
        whole(`linear-gradient(90deg, transparent 2.25rem, ${m} 2.25rem, ${m} calc(2.25rem + 2px), transparent calc(2.25rem + 2px))`),
        tile(`linear-gradient(to bottom, transparent ${px(s - w)}, ${c} ${px(s - w)})`, s, s),
      ].map((l, i) => (i === 1 ? { ...l, size: `100% ${px(s)}`, to: `0 ${px(s)}` } : l));
    }
    case "horizon":
      return [{ ...whole(svgURL(horizonSVG(b, ink, second, palette, o))), pos: "50% 50%" }];
    case "spots": {
      // Soft circles that fade out where the next begins, so the colours
      // blend; sized in pixels, so a long page does not stretch them.
      const r = s * 3;
      return (b.points ?? []).slice(0, BACKDROP_LIMITS.spots).map((p, i) => {
        const c = p.color && HEX.test(p.color) ? p.color : palette[i % palette.length]!;
        return whole(`radial-gradient(circle ${px(r)} at ${clamp(p.x, 0, 100)}% ${clamp(p.y, 0, 100)}%, ${rgba(c, o)}, ${rgba(c, 0)})`);
      });
    }
    case "waves": {
      const w = s * 2;
      const h = s;
      const sw = Math.max(1.5, s / 12);
      const d = `M0 ${h / 2} Q ${w / 4} ${h / 2 - h * 0.35} ${w / 2} ${h / 2} T ${w} ${h / 2}`;
      return [tile(svgURL(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><path d="${d}" fill="none" stroke="${ink}" stroke-opacity="${o}" stroke-width="${sw}" stroke-linecap="round"/></svg>`), w, h)];
    }
    case "zigzag": {
      const w = s;
      const h = s * 0.6;
      const sw = Math.max(1.5, s / 12);
      const pts = `0,${h * 0.75} ${w / 4},${h * 0.25} ${w / 2},${h * 0.75} ${(3 * w) / 4},${h * 0.25} ${w},${h * 0.75}`;
      return [tile(svgURL(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><polyline points="${pts}" fill="none" stroke="${ink}" stroke-opacity="${o}" stroke-width="${sw}" stroke-linejoin="round"/></svg>`), w, h)];
    }
    case "confetti": {
      // Pieces stay clear of the tile's edge, so none is cut at a seam.
      const t = s * 4;
      const rnd = seeded(7);
      const list = colourList(b);
      const colours = list.length ? list : [ink, second];
      const bits: string[] = [];
      for (let i = 0; i < 18; i++) {
        const x = s * 0.3 + rnd() * (t - s * 0.6);
        const y = s * 0.3 + rnd() * (t - s * 0.6);
        const fill = colours[i % colours.length];
        if (rnd() < 0.35) bits.push(`<circle cx="${x.toFixed(1)}" cy="${y.toFixed(1)}" r="${(s * 0.12).toFixed(1)}" fill="${fill}"/>`);
        else {
          const w = s * 0.42;
          const h = s * 0.15;
          bits.push(`<rect x="${(x - w / 2).toFixed(1)}" y="${(y - h / 2).toFixed(1)}" width="${w.toFixed(1)}" height="${h.toFixed(1)}" rx="${(h / 3).toFixed(1)}" fill="${fill}" transform="rotate(${Math.round(rnd() * 180)} ${x.toFixed(1)} ${y.toFixed(1)})"/>`);
        }
      }
      return [tile(svgURL(`<svg xmlns="http://www.w3.org/2000/svg" width="${t}" height="${t}"><g opacity="${o}">${bits.join("")}</g></svg>`), t, t)];
    }
    case "snow": {
      // Snowflakes, stars and specks, at most one to each cell of a 6 × 6
      // grid so none clump or touch the tile's edge, with a few cells left
      // empty so it reads as falling snow, not wallpaper. The drift falls
      // straight down.
      const n = 6;
      const t = s * n;
      const rnd = seeded(11);
      const list = colourList(b);
      const colours = list.length ? list : [ink, second];
      const sw = Math.max(1, s / 16).toFixed(1);
      const bits: string[] = [];
      for (let i = 0; i < n * n; i++) {
        const x = s * ((i % n) + 0.5 + (rnd() - 0.5) * 0.4);
        const y = s * (Math.floor(i / n) + 0.5 + (rnd() - 0.5) * 0.4);
        const fill = colours[i % colours.length];
        const pick = rnd();
        if (pick > 0.8) continue;
        const at = (r: number, deg: number) => [x + r * Math.cos((deg * Math.PI) / 180), y + r * Math.sin((deg * Math.PI) / 180)].map((v) => v.toFixed(1));
        if (pick < 0.4) {
          const r = s * (0.18 + rnd() * 0.12);
          const spin = rnd() * 60;
          const arms: string[] = [];
          for (let k = 0; k < 6; k++) {
            const d = spin + k * 60;
            const [ex, ey] = at(r, d);
            const [bx, by] = at(r * 0.55, d);
            const [lx, ly] = at(r * 0.85, d - 25);
            const [rx, ry] = at(r * 0.85, d + 25);
            arms.push(`M${x.toFixed(1)} ${y.toFixed(1)}L${ex} ${ey}M${lx} ${ly}L${bx} ${by}L${rx} ${ry}`);
          }
          bits.push(`<path d="${arms.join("")}" fill="none" stroke="${fill}" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round"/>`);
        } else if (pick < 0.62) {
          const r = s * (0.14 + rnd() * 0.08);
          const pts: string[] = [];
          for (let k = 0; k < 10; k++) pts.push(at(k % 2 ? r * 0.45 : r, -90 + k * 36).join(","));
          bits.push(`<polygon points="${pts.join(" ")}" fill="${fill}"/>`);
        } else bits.push(`<circle cx="${x.toFixed(1)}" cy="${y.toFixed(1)}" r="${(s * (0.05 + rnd() * 0.05)).toFixed(1)}" fill="${fill}"/>`);
      }
      return [{ ...tile(svgURL(`<svg xmlns="http://www.w3.org/2000/svg" width="${t}" height="${t}"><g opacity="${o}">${bits.join("")}</g></svg>`), t, t), to: `0px ${px(t)}` }];
    }
  }
  return [];
}

/**
 * The colours a backdrop takes when given none of its own: the accent,
 * the titles colour and the answer bar — never the text colour, which
 * makes a grey smudge, and none too close to the page to be seen. Colour
 * spots cycle through them; a glow runs from the first to the second, or
 * into the page when there is only one.
 */
export function backdropPalette(brand: play.Brand | null | undefined): string[] {
  const hex = (v: string | undefined) => (v && HEX.test(v) ? v.toLowerCase() : undefined);
  const page = hex(brand?.background) ?? "#fbfbfd";
  const accent = hex(brand?.accent) ?? DEFAULT_ACCENT;
  const out = [accent, hex(brand?.accent_secondary), hex(brand?.submit_bg)].filter(
    (c, i, all): c is string => !!c && all.indexOf(c) === i && visibleOn(c, page),
  );
  return out.length ? out : [accent];
}

/** Whether a colour stands apart from the page at all (WCAG ratio above 1.15). */
function visibleOn(c: string, page: string): boolean {
  const lum = (h: string) => {
    const n = parseInt(h.slice(1), 16);
    const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    }) as [number, number, number];
    return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
  };
  const [a, b] = [lum(c), lum(page)].sort((x, y) => y - x) as [number, number];
  return (a + 0.05) / (b + 0.05) > 1.15;
}

/**
 * The custom properties for a backdrop over a palette, or none for no
 * backdrop. An empty colour follows the palette: a pattern is drawn in
 * the text colour; confetti, glows and spots, which want colour, take
 * backdropPalette's.
 */
export function backdropVars(b: play.Backdrop | null | undefined, brand: play.Brand | null | undefined): Record<string, string> {
  if (!b || !(b.kind in BACKDROP_KINDS)) return {};
  if (b.kind === "picture") return pictureVars(b);
  const hex = (v: string | undefined) => (v && HEX.test(v) ? v : undefined);
  const text = hex(brand?.text) ?? DEFAULT_INK;
  const page = hex(brand?.background) ?? "#fbfbfd";
  const palette = backdropPalette(brand);
  const colourful = b.kind === "confetti" || b.kind === "snow" || b.kind === "gradient";
  // A horizon's grid takes the second palette colour and its sun a warm gold, unless given.
  if (b.kind === "horizon") {
    const ls = layers(b, hex(b.color) ?? palette[1] ?? "#2de2e6", hex(b.color2) ?? "#ffd319", palette);
    return finish(b, ls);
  }
  const first = hex(b.color) ?? (colourful ? palette[0]! : text);
  // With one colour to go on, confetti pairs it with a lighter shade of itself.
  const second = hex(b.color2) ?? (b.kind === "gradient" ? (palette[1] ?? page) : (b.kind === "confetti" || b.kind === "snow") ? (palette[1] ?? mix(first, page, 0.5)) : palette[0]!);
  const ls = layers(b, first, second, palette);
  return finish(b, ls);
}

/** The layers as custom properties, with a fade on top and the drift when asked for. */
function finish(b: play.Backdrop, ls: Layer[]): Record<string, string> {
  if (!ls.length) return {};
  if (b.fade) ls.unshift({ ...whole("linear-gradient(to bottom, transparent 30%, var(--bg) 92%)") });
  const list = (k: keyof Layer) => ls.map((l) => l[k]).join(", ");
  const out: Record<string, string> = {
    "--pv-backdrop": list("image"),
    "--pv-backdrop-size": list("size"),
    "--pv-backdrop-pos": list("pos"),
    "--pv-backdrop-repeat": list("repeat"),
    "--pv-backdrop-attach": ls.map(() => (b.fixed ? "fixed" : "scroll")).join(", "),
  };
  if (b.motion && BACKDROP_KINDS[b.kind].motion) {
    out["--pv-backdrop-to"] = list("to");
    out["--pv-backdrop-anim"] = "pv-backdrop-drift 40s linear infinite";
  }
  return out;
}
