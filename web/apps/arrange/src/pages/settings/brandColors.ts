/**
 * Colours for an organisation's brand, picked from its logo, and the
 * contrast checks that keep its pages readable. Runs in the browser:
 * the logo is drawn small on a canvas and its pixels counted.
 */

export interface BrandColors {
  accent_color: string;
  accent_secondary_color: string;
  background_color: string;
  text_color: string;
}

type RGB = [number, number, number];

function hex([r, g, b]: RGB): string {
  return "#" + [r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, "0")).join("");
}

export function parseHex(h: string): RGB {
  const s = h.replace("#", "");
  const f = s.length === 3 ? s.split("").map((c) => c + c).join("") : s;
  return [0, 2, 4].map((i) => Number.parseInt(f.slice(i, i + 2), 16)) as RGB;
}

function hsl([r, g, b]: RGB): [number, number, number] {
  const [R, G, B] = [r / 255, g / 255, b / 255];
  const max = Math.max(R, G, B);
  const min = Math.min(R, G, B);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h = max === R ? (G - B) / d + (G < B ? 6 : 0) : max === G ? (B - R) / d + 2 : (R - G) / d + 4;
  h /= 6;
  return [h, s, l];
}

function fromHsl(h: number, s: number, l: number): RGB {
  const f = (n: number) => {
    const k = (n + h * 12) % 12;
    const a = s * Math.min(l, 1 - l);
    return 255 * (l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1)));
  };
  return [f(0), f(8), f(4)];
}

function luminance([r, g, b]: RGB): number {
  const c = [r, g, b].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
}

/** WCAG contrast ratio of two hex colours, 1 to 21. */
export function contrast(a: string, b: string): number {
  const la = luminance(parseHex(a));
  const lb = luminance(parseHex(b));
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/**
 * fg moved darker or lighter, keeping its hue, until it reads on bg at
 * WCAG AA (4.5:1); fg itself when it already does.
 */
export function fixContrast(fg: string, bg: string, ratio = 4.5): string {
  if (contrast(fg, bg) >= ratio) return fg;
  const [h, s, l0] = hsl(parseHex(fg));
  const darker = luminance(parseHex(bg)) > 0.4;
  for (let i = 1; i <= 40; i++) {
    const l = darker ? l0 - i * 0.025 : l0 + i * 0.025;
    const c = hex(fromHsl(h, s, Math.max(0, Math.min(1, l))));
    if (contrast(c, bg) >= ratio) return c;
  }
  return darker ? "#000000" : "#ffffff";
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("the logo could not be read"));
    img.src = src;
  });
}

/**
 * Colours suggested from the logo at src: its most common strong colour
 * as the accent, the next one apart from it as the second, a calm
 * surface tinted by the accent and an ink that reads on it.
 */
export async function colorsFromLogo(src: string): Promise<BrandColors> {
  const img = await loadImage(src);
  const size = 64;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) throw new Error("the logo could not be read");
  const scale = Math.min(size / img.naturalWidth, size / img.naturalHeight, 1) || 1;
  ctx.drawImage(img, 0, 0, img.naturalWidth * scale, img.naturalHeight * scale);
  const data = ctx.getImageData(0, 0, size, size).data;
  const buckets = new Map<string, { n: number; sum: RGB }>();
  for (let i = 0; i < data.length; i += 4) {
    if (data[i + 3]! < 200) continue;
    const px: RGB = [data[i]!, data[i + 1]!, data[i + 2]!];
    const [, s, l] = hsl(px);
    if (s < 0.25 || l < 0.12 || l > 0.9) continue;
    const key = px.map((v) => v >> 5).join(",");
    const b = buckets.get(key) ?? { n: 0, sum: [0, 0, 0] as RGB };
    b.n++;
    b.sum = [b.sum[0] + px[0], b.sum[1] + px[1], b.sum[2] + px[2]];
    buckets.set(key, b);
  }
  const ranked = [...buckets.values()].sort((a, b) => b.n - a.n).map((b) => b.sum.map((v) => v / b.n) as RGB);
  const first = ranked[0];
  if (!first) throw new Error("the logo has no colours to pick from: choose them below");
  const hue = hsl(first)[0];
  const second = ranked.find((c) => {
    const d = Math.abs(hsl(c)[0] - hue);
    return Math.min(d, 1 - d) > 0.06;
  });
  const [h, s] = hsl(first);
  const background = hex(fromHsl(h, Math.min(s, 0.35), 0.975));
  const text = hex(fromHsl(h, Math.min(s, 0.45), 0.16));
  const accent = fixContrast(hex(first), "#ffffff", 3);
  const accent2 = second ? hex(second) : hex(fromHsl((h + 0.08) % 1, s, Math.min(0.6, hsl(first)[2] + 0.12)));
  return { accent_color: accent, accent_secondary_color: accent2, background_color: background, text_color: text };
}

/**
 * The store hero's background (internal/server/web/store.html, .hero): a
 * dark shade over the cover, darker towards the bottom, and only a faint
 * accent glow from the top left, so the picture shows as it is. Without
 * a cover, the organisation's colours on dark. Keep the two in step.
 */
export function storeHeroBackground(cover: string, accent: string, accent2: string): string {
  return [
    "linear-gradient(180deg, rgba(12, 14, 20, 0.3) 0%, rgba(12, 14, 20, 0.55) 55%, rgba(12, 14, 20, 0.92) 100%)",
    `linear-gradient(120deg, color-mix(in srgb, ${accent} 55%, transparent) 0%, transparent 65%)`,
    cover
      ? `url('${cover}') center / cover`
      : `radial-gradient(120% 120% at 85% 10%, color-mix(in srgb, ${accent2} 70%, #131722) 0%, #131722 60%)`,
  ].join(", ") + ", #131722";
}
