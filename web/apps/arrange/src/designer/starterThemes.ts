import type { ThemeLook } from "@zonventure/graph";

/**
 * Whole looks to start a theme from, for authors who would rather adjust
 * one than build one: colours, fonts, shapes, a background and cards.
 * Copied into the adventure when picked, so changing one here leaves the
 * themes already made from it alone. Each reads well outdoors
 * (starters.test.ts checks them with play-ui's lookContrast).
 */
/** The gallery's sections, in order. */
export const STARTER_GROUPS = [
  { id: "playful", label: "Playful" },
  { id: "adventure", label: "Adventure" },
  // Shown without a title, after the others.
  { id: "grownup", label: "" },
] as const;

export const STARTER_THEMES: { id: string; label: string; group: (typeof STARTER_GROUPS)[number]["id"]; look: ThemeLook }[] = [
  {
    // Sunlit leaves: light greens and a warm sun, deep-orange titles.
    id: "jungle",
    group: "adventure",
    label: "Jungle",
    look: {
      background: "#f1f6e9", text: "#1b2e12", accent: "#2e7d32", accent_secondary: "#bf360c", submit_bg: "#dcedc8",
      font: "baloo-2", heading_font: "chewy",
      backdrop: { kind: "spots", opacity: 0.4, scale: 110, points: [{ x: 8, y: 4, color: "#aed581" }, { x: 92, y: 30, color: "#ffe082" }, { x: 18, y: 88, color: "#81c784" }] },
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient" },
    },
  },
  {
    // A nebula on deep night blue, frosted cards, sky-blue titles.
    id: "space",
    group: "adventure",
    label: "Space",
    look: {
      background: "#0b0d26", text: "#eceeff", accent: "#7c4dff", accent_secondary: "#4fc3f7", submit_bg: "#171a3f",
      font: "fredoka", heading_font: "luckiest-guy",
      backdrop: { kind: "spots", opacity: 0.55, scale: 110, points: [{ x: 12, y: 18, color: "#4527a0" }, { x: 88, y: 55, color: "#1565c0" }, { x: 35, y: 96, color: "#6a1b9a" }] },
      style: { card_opacity: 0.75, card_blur: true, transition: "zoom", button_fill: "gradient" },
    },
  },
  {
    // A case file: aged paper, oxblood stamps, a typewriter for titles only.
    id: "detective",
    group: "adventure",
    label: "Detective",
    look: {
      background: "#f3ebd9", text: "#2b2118", accent: "#8b2e16", accent_secondary: "#3e2c1c", submit_bg: "#e5d6b3",
      font: "merriweather", heading_font: "special-elite",
      corners: "sharp", cards: "flat", card_outline: true,
      backdrop: { kind: "grid", opacity: 0.08, scale: 24 },
      style: { button_shape: "square", button_fill: "flat", title_upper: true },
    },
  },
  {
    // A sea chart: parchment, navy waves, a red flag for buttons.
    id: "pirate",
    group: "adventure",
    label: "Pirate",
    look: {
      background: "#f1e2bf", text: "#33210f", accent: "#a61e1a", accent_secondary: "#173f6e", submit_bg: "#dfc48a",
      font: "merriweather", heading_font: "luckiest-guy",
      corners: "soft", card_outline: true,
      backdrop: { kind: "waves", color: "#173f6e", opacity: 0.1, scale: 40 },
      style: { button_fill: "gradient" },
    },
  },
  {
    // Sweet shop: candy stripes, raspberry buttons, grape titles.
    id: "candy",
    group: "playful",
    label: "Candy",
    look: {
      background: "#fff1f7", text: "#4a1838", accent: "#d81b60", accent_secondary: "#8e24aa", submit_bg: "#ffd9ea",
      font: "fredoka", heading_font: "chewy",
      cards: "flat",
      backdrop: { kind: "stripes", color: "#f06292", opacity: 0.08, scale: 40, angle: 135 },
      style: { dock: "floating", title_style: "ribbon", button_fill: "gradient" },
    },
  },
  {
    // Shallow water: soft blues and teals, deep-teal titles.
    id: "ocean",
    group: "adventure",
    label: "Ocean",
    look: {
      background: "#e8f6fb", text: "#0b2f40", accent: "#0277bd", accent_secondary: "#00695c", submit_bg: "#cdeef8",
      font: "baloo-2", heading_font: "baloo-2",
      backdrop: { kind: "spots", opacity: 0.4, scale: 110, points: [{ x: 14, y: 8, color: "#81d4fa" }, { x: 86, y: 48, color: "#80cbc4" }, { x: 28, y: 92, color: "#4fc3f7" }] },
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient" },
    },
  },
  {
    // Pastel magic: a soft rainbow of spots, fuchsia buttons, violet titles.
    id: "unicorn",
    group: "playful",
    label: "Unicorn",
    look: {
      background: "#fdf2ff", text: "#3b1a5a", accent: "#a21caf", accent_secondary: "#7c3aed", submit_bg: "#f3e3ff",
      font: "fredoka", heading_font: "chewy",
      backdrop: { kind: "spots", opacity: 0.55, scale: 100, points: [
        { x: 10, y: 5, color: "#f9a8d4" }, { x: 90, y: 22, color: "#c4b5fd" }, { x: 14, y: 55, color: "#99f6e4" }, { x: 86, y: 76, color: "#fde68a" }, { x: 40, y: 98, color: "#a5d8ff" },
      ] },
      style: { card_opacity: 0.85, card_blur: true, transition: "pop", dock: "floating", button_fill: "soft" },
    },
  },
  {
    // A rainbow across a white page, fading out under the text.
    id: "rainbow",
    group: "playful",
    label: "Rainbow",
    look: {
      background: "#ffffff", text: "#1f2937", accent: "#2563eb", accent_secondary: "#86198f", submit_bg: "#eef2ff",
      font: "baloo-2", heading_font: "luckiest-guy",
      backdrop: { kind: "stripes", opacity: 0.3, scale: 70, angle: 135, fade: true, colors: ["#ff595e", "#ffca3a", "#8ac926", "#1982c4", "#6a4c93"] },
      style: { button_fill: "gradient" },
    },
  },
  {
    // Kids' party: confetti in every colour, drifting slowly.
    id: "party",
    group: "playful",
    label: "Party",
    look: {
      background: "#fffaf0", text: "#2d1b4e", accent: "#db2777", accent_secondary: "#6d28d9", submit_bg: "#ffe4f1",
      font: "fredoka", heading_font: "luckiest-guy",
      backdrop: { kind: "confetti", opacity: 0.5, scale: 26, motion: true, colors: ["#ff3d7f", "#ffb703", "#2ec4b6", "#8338ec", "#3a86ff"] },
      style: { transition: "pop", button_size: "large", title_style: "ribbon", button_fill: "gradient" },
    },
  },
  {
    // A sunny day: golden rays from the top, warm orange buttons.
    id: "sunshine",
    group: "playful",
    label: "Sunshine",
    look: {
      background: "#fffbe8", text: "#3b2a00", accent: "#c2410c", accent_secondary: "#b45309", submit_bg: "#ffefb3",
      font: "baloo-2", heading_font: "chewy",
      backdrop: { kind: "sunburst", color: "#fbbf24", opacity: 0.22, scale: 60, fade: true },
      style: { button_fill: "gradient" },
    },
  },
  {
    // Ice cream parlour: strawberry dots, mint answer bar.
    id: "ice-cream",
    group: "playful",
    label: "Ice cream",
    look: {
      background: "#fff6f9", text: "#47202f", accent: "#be185d", accent_secondary: "#0f766e", submit_bg: "#d9f5f0",
      font: "fredoka", heading_font: "chewy",
      backdrop: { kind: "dots", color: "#f472b6", opacity: 0.18, scale: 30 },
      style: { button_size: "large", button_fill: "gradient" },
    },
  },
  {
    // A terminal: green on black, scan lines, pixel titles.
    id: "hacker",
    group: "grownup",
    label: "Hacker",
    look: {
      background: "#050a05", text: "#b9f6b9", accent: "#22c55e", accent_secondary: "#39ff14", submit_bg: "#0b170b",
      font: "inconsolata", heading_font: "vt323",
      corners: "sharp", cards: "flat", card_outline: true,
      backdrop: { kind: "stripes", color: "#39ff14", opacity: 0.05, scale: 8, angle: 0 },
      style: { transition: "fade", button_shape: "square", button_fill: "outline" },
    },
  },
  {
    // Clean and calm: cool greys, a business blue, the standard typeface.
    id: "office",
    group: "grownup",
    label: "Office",
    look: {
      background: "#f5f7fa", text: "#1f2a37", accent: "#1d4ed8", accent_secondary: "#1e3a8a", submit_bg: "#e8edf4",
      font: "inter", heading_font: "inter",
      corners: "soft",
      style: { button_shape: "rounded", button_fill: "flat" },
    },
  },
  {
    // Something in the dark: blood-red shadows, frosted cards, dripping titles.
    id: "scary",
    group: "grownup",
    label: "Scary",
    look: {
      background: "#0d0505", text: "#f2e6e6", accent: "#b91c1c", accent_secondary: "#f87171", submit_bg: "#1f0a0a",
      font: "merriweather", heading_font: "creepster",
      corners: "sharp",
      backdrop: { kind: "spots", opacity: 0.7, scale: 110, points: [{ x: 18, y: 10, color: "#5c0a0a" }, { x: 86, y: 58, color: "#3b0d2e" }, { x: 30, y: 96, color: "#4a0505" }] },
      style: { card_opacity: 0.75, card_blur: true, transition: "fade", button_fill: "gradient" },
    },
  },
  {
    // A rainy day: blue-greys, slanting rain, handwritten titles.
    id: "sad",
    group: "grownup",
    label: "Sad",
    look: {
      background: "#e9edf2", text: "#26323f", accent: "#44617b", accent_secondary: "#3b4f63", submit_bg: "#d6dde6",
      font: "nunito", heading_font: "patrick-hand",
      backdrop: { kind: "stripes", color: "#6b7f95", opacity: 0.06, scale: 12, angle: 75, fade: true },
      style: { button_fill: "gradient" },
    },
  },
  {
    // A love letter: blush spots, rose buttons, script titles.
    id: "love",
    group: "grownup",
    label: "Love",
    look: {
      background: "#fff1f3", text: "#4c0519", accent: "#be123c", accent_secondary: "#be123c", submit_bg: "#ffe0e6",
      font: "nunito", heading_font: "pacifico",
      backdrop: { kind: "spots", opacity: 0.5, scale: 110, points: [{ x: 15, y: 10, color: "#fda4af" }, { x: 85, y: 45, color: "#f9a8d4" }, { x: 30, y: 92, color: "#fecdd3" }] },
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient" },
    },
  },
  {
    // Night city: neon pink and cyan on near-black, a glowing grid.
    id: "cyberpunk",
    group: "grownup",
    label: "Cyberpunk",
    look: {
      background: "#0a0614", text: "#ece6ff", accent: "#ff2a6d", accent_secondary: "#05d9e8", submit_bg: "#160c28",
      font: "inter", heading_font: "orbitron",
      corners: "sharp", cards: "flat", card_outline: true,
      backdrop: { kind: "grid", color: "#05d9e8", opacity: 0.16, scale: 28 },
      style: { card_opacity: 0.8, card_blur: true, transition: "slide", button_shape: "square", button_fill: "outline", title_upper: true },
    },
  },
  {
    // Synthwave: a starry purple sky, a striped sun, a neon grid to the horizon.
    id: "retro-80s",
    group: "grownup",
    label: "Retro 80s",
    look: {
      background: "#1c0e31", text: "#fbeaff", accent: "#ff4fa3", accent_secondary: "#ffd319", submit_bg: "#1c0e31",
      font: "nunito", heading_font: "press-start-2p",
      corners: "sharp",
      backdrop: { kind: "horizon", opacity: 1, scale: 40, color: "#2de2e6", color2: "#ffd319", fixed: true, colors: ["#1c0e31", "#2e1f56", "#59255b", "#d74789"] },
      style: { card_opacity: 0.7, card_blur: true, transition: "slide", button_shape: "square", title_style: "shadow", dock: "floating", button_fill: "gradient" },
    },
  },
  {
    // A school notebook: ruled lines, a red margin, ballpoint blue, red-pen titles.
    id: "pen-and-paper",
    group: "adventure",
    label: "Pen & paper",
    look: {
      background: "#fdfcf7", text: "#1f2a44", accent: "#1e4fa3", accent_secondary: "#c62828", submit_bg: "#f1efe6",
      font: "patrick-hand", heading_font: "patrick-hand",
      corners: "soft",
      backdrop: { kind: "lined", color: "#5b8fd4", color2: "#e57373", opacity: 0.22, scale: 30 },
      style: { card_opacity: 0.6, transition: "flip", title_style: "underline", button_fill: "flat" },
    },
  },
];
