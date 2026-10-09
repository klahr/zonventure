import type { play } from "@zonventure/api";

/** A whole look: the same shape as @zonventure/graph's ThemeLook. */
type ThemeLook = play.Brand;

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
  { id: "seasonal", label: "Seasonal" },
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
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient", confetti_emoji: "🐘🍌🌴" },
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
      style: { card_opacity: 0.75, card_blur: true, transition: "zoom", button_fill: "gradient", confetti_emoji: "⭐🚀" },
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
      style: { dock: "floating", title_style: "ribbon", button_fill: "gradient", confetti_emoji: "🍬🍭" },
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
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient", confetti_emoji: "🐠🐚" },
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
      style: { card_opacity: 0.85, card_blur: true, transition: "pop", dock: "floating", button_fill: "soft", confetti_emoji: "🦄🌈✨" },
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
      style: { button_fill: "gradient", confetti_colors: ["#ff595e", "#ffca3a", "#8ac926", "#1982c4", "#6a4c93"] },
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
      style: { transition: "pop", button_size: "large", title_style: "ribbon", button_fill: "gradient", confetti_colors: ["#ff3d7f", "#ffb703", "#2ec4b6", "#8338ec", "#3a86ff"] },
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
      style: { button_size: "large", button_fill: "gradient", confetti_emoji: "🍦🍓" },
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
      style: { transition: "fade", button_shape: "square", button_fill: "outline", celebrate: "quiet", cheer_text: "ACCESS GRANTED", miss_text: "ACCESS DENIED" },
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
      style: { button_shape: "rounded", button_fill: "flat", celebrate: "quiet" },
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
      style: { card_opacity: 0.75, card_blur: true, transition: "fade", button_fill: "gradient", confetti_emoji: "🦇👻" },
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
      style: { button_fill: "gradient", celebrate: "quiet" },
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
      style: { card_opacity: 0.85, card_blur: true, button_fill: "gradient", confetti_emoji: "💖💌" },
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
      style: { card_opacity: 0.8, card_blur: true, transition: "slide", button_shape: "square", button_fill: "outline", title_upper: true, confetti_colors: ["#ff2a6d", "#05d9e8", "#d1f7ff"] },
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
      style: { card_opacity: 0.7, card_blur: true, transition: "slide", button_shape: "square", title_style: "shadow", dock: "floating", button_fill: "gradient", confetti_colors: ["#ff4fa3", "#ffd319", "#2de2e6"] },
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
      style: { card_opacity: 0.6, transition: "flip", title_style: "underline", button_fill: "flat", confetti_emoji: "✏️⭐" },
    },
  },
  {
    // A Christmas night: deep pine green, falling snow, red buttons, gold titles.
    id: "christmas",
    group: "seasonal",
    label: "Christmas",
    look: {
      background: "#0f2a1d", text: "#fdf6ec", accent: "#d62828", accent_secondary: "#f2c14e", submit_bg: "#173826",
      font: "fredoka", heading_font: "pacifico",
      backdrop: { kind: "snow", opacity: 0.5, scale: 30, motion: true, colors: ["#ffffff", "#f2c14e"] },
      style: { card_opacity: 0.8, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🎄🎁⭐" },
    },
  },
  {
    // Trick or treat: a purple night, a pumpkin-orange glow, slime-green titles.
    id: "halloween",
    group: "seasonal",
    label: "Halloween",
    look: {
      background: "#1a0f2e", text: "#fff4e6", accent: "#f97316", accent_secondary: "#a3e635", submit_bg: "#2a1846",
      font: "fredoka", heading_font: "creepster",
      backdrop: { kind: "spots", opacity: 0.6, scale: 110, points: [{ x: 88, y: 12, color: "#c2410c" }, { x: 10, y: 55, color: "#5b21b6" }, { x: 45, y: 98, color: "#3b0764" }] },
      style: { card_opacity: 0.8, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🎃👻🦇" },
    },
  },
  {
    // Spring morning: pale yellow, pastel eggs, pink buttons, fresh-green titles.
    id: "easter",
    group: "seasonal",
    label: "Easter",
    look: {
      background: "#fffbea", text: "#3b2f12", accent: "#db2777", accent_secondary: "#15803d", submit_bg: "#fdf0b8",
      font: "baloo-2", heading_font: "chewy",
      backdrop: { kind: "spots", opacity: 0.5, scale: 110, points: [{ x: 10, y: 8, color: "#fde68a" }, { x: 90, y: 35, color: "#fbcfe8" }, { x: 15, y: 70, color: "#bbf7d0" }, { x: 80, y: 95, color: "#ddd6fe" }] },
      style: { card_opacity: 0.85, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🐣🥚🌷" },
    },
  },
  {
    // A light summer night: meadow flowers, cornflower-blue buttons, leaf-green titles.
    id: "midsummer",
    group: "seasonal",
    label: "Midsummer",
    look: {
      background: "#f6fbef", text: "#1f2d16", accent: "#1e5aa8", accent_secondary: "#3f7d20", submit_bg: "#e3f0d3",
      font: "baloo-2", heading_font: "patrick-hand",
      backdrop: { kind: "spots", opacity: 0.45, scale: 110, points: [{ x: 8, y: 6, color: "#fde047" }, { x: 92, y: 28, color: "#93c5fd" }, { x: 12, y: 65, color: "#f9a8d4" }, { x: 85, y: 92, color: "#bef264" }] },
      style: { card_opacity: 0.85, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🌼🍓🌸" },
    },
  },
  {
    // A beach holiday: warm sand, gentle waves, turquoise buttons, coral titles.
    id: "summer",
    group: "seasonal",
    label: "Summer",
    look: {
      background: "#fff7e6", text: "#2b2112", accent: "#0e7490", accent_secondary: "#d9480f", submit_bg: "#d5f2f3",
      font: "fredoka", heading_font: "luckiest-guy",
      backdrop: { kind: "waves", color: "#0e7490", opacity: 0.1, scale: 40 },
      style: { transition: "pop", dock: "floating", button_fill: "gradient", confetti_emoji: "🏖️🍉⛱️" },
    },
  },
  {
    // A crisp winter day: icy blue, light snow, a hint of northern lights.
    id: "winter",
    group: "seasonal",
    label: "Winter",
    look: {
      background: "#eef6fc", text: "#12304a", accent: "#1d6fb8", accent_secondary: "#5b4bc4", submit_bg: "#dcebf7",
      font: "nunito", heading_font: "fredoka",
      backdrop: { kind: "snow", opacity: 0.5, scale: 30, motion: true, colors: ["#93c5fd", "#c7d2fe"] },
      style: { card_opacity: 0.85, card_blur: true, transition: "fade", button_fill: "gradient", confetti_emoji: "❄️⛄" },
    },
  },
  {
    // First warm days: a pale sky, drifting blossom petals, green buttons, magenta titles.
    id: "spring",
    group: "seasonal",
    label: "Spring",
    look: {
      background: "#f2f8ff", text: "#1d2e22", accent: "#2f855a", accent_secondary: "#b83280", submit_bg: "#e1f2e6",
      font: "nunito", heading_font: "pacifico",
      backdrop: { kind: "confetti", opacity: 0.35, scale: 26, motion: true, colors: ["#f9a8d4", "#fbcfe8", "#bbf7d0"] },
      style: { card_opacity: 0.85, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🌸🌱🐝" },
    },
  },
  {
    // Autumn leaves: warm parchment, falling leaves, rust buttons, deep-plum titles.
    id: "autumn",
    group: "seasonal",
    label: "Autumn",
    look: {
      background: "#fbf3e8", text: "#3a2414", accent: "#b4441c", accent_secondary: "#7a2e3a", submit_bg: "#f1dfc6",
      font: "merriweather", heading_font: "chewy",
      backdrop: { kind: "confetti", opacity: 0.35, scale: 30, motion: true, colors: ["#d9480f", "#e9a23b", "#9a3412"] },
      style: { card_opacity: 0.85, card_blur: true, transition: "fade", button_fill: "gradient", confetti_emoji: "🍂🍁🍄" },
    },
  },
  {
    // A Valentine's party: deep wine red, pink and red confetti, hot-pink buttons, blush titles.
    id: "valentines",
    group: "seasonal",
    label: "Valentine's Day",
    look: {
      background: "#4a0d1f", text: "#fff0f5", accent: "#ff4d6d", accent_secondary: "#ffb3c6", submit_bg: "#5e1328",
      font: "fredoka", heading_font: "luckiest-guy",
      backdrop: { kind: "confetti", opacity: 0.4, scale: 28, motion: true, colors: ["#ff4d6d", "#ffb3c6", "#ffffff"] },
      style: { card_opacity: 0.8, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "💘🌹💝" },
    },
  },
  {
    // Midnight countdown: a deep night sky, gold and silver confetti, gold titles.
    id: "new-year",
    group: "seasonal",
    label: "New Year's Eve",
    look: {
      background: "#0d0b1e", text: "#f7f3e8", accent: "#b8860b", accent_secondary: "#f5d76e", submit_bg: "#1a1733",
      font: "nunito", heading_font: "pacifico",
      backdrop: { kind: "confetti", opacity: 0.45, scale: 26, motion: true, colors: ["#f5d76e", "#c0c0c0", "#ffffff"] },
      style: { card_opacity: 0.8, card_blur: true, transition: "zoom", button_fill: "gradient", confetti_emoji: "🎆✨🎉" },
    },
  },
  {
    // Lucia morning: dark winter blue, candle glow, a red sash for buttons, candle-gold titles.
    id: "lucia",
    group: "seasonal",
    label: "Lucia",
    look: {
      background: "#0f1a33", text: "#fdf8ec", accent: "#c62828", accent_secondary: "#ffd27a", submit_bg: "#18264a",
      font: "nunito", heading_font: "merriweather",
      backdrop: { kind: "spots", opacity: 0.55, scale: 110, points: [{ x: 86, y: 10, color: "#b7791f" }, { x: 12, y: 58, color: "#1e3a8a" }, { x: 50, y: 98, color: "#7c2d12" }] },
      style: { card_opacity: 0.8, card_blur: true, transition: "fade", button_fill: "gradient", confetti_emoji: "🕯️⭐" },
    },
  },
  {
    // A crayfish party: a warm August evening, crayfish-red buttons, navy titles.
    id: "crayfish",
    group: "seasonal",
    label: "Crayfish party",
    look: {
      background: "#fffaf0", text: "#1f2a44", accent: "#d62828", accent_secondary: "#1d4e89", submit_bg: "#fde9c8",
      font: "fredoka", heading_font: "luckiest-guy",
      backdrop: { kind: "dots", color: "#f4a261", opacity: 0.18, scale: 30 },
      style: { transition: "pop", title_style: "ribbon", button_fill: "gradient", confetti_emoji: "🦞🌕🍋" },
    },
  },
  {
    // Walpurgis Night: a spring evening round the bonfire, fire-orange buttons, flame-yellow titles.
    id: "walpurgis",
    group: "seasonal",
    label: "Walpurgis Night",
    look: {
      background: "#1b1420", text: "#fff4e6", accent: "#e8590c", accent_secondary: "#ffd43b", submit_bg: "#2a1f2e",
      font: "baloo-2", heading_font: "chewy",
      backdrop: { kind: "spots", opacity: 0.6, scale: 110, points: [{ x: 80, y: 95, color: "#c2410c" }, { x: 20, y: 98, color: "#9a3412" }, { x: 60, y: 10, color: "#3b2a5a" }] },
      style: { card_opacity: 0.8, card_blur: true, transition: "pop", button_fill: "gradient", confetti_emoji: "🔥🌸" },
    },
  },
  {
    // A chalkboard: dark green, chalk-white text, pencil-yellow buttons, handwritten titles.
    id: "back-to-school",
    group: "seasonal",
    label: "Back to school",
    look: {
      background: "#1f3a2e", text: "#f4f1e8", accent: "#f2a541", accent_secondary: "#ffd166", submit_bg: "#284a3b",
      font: "patrick-hand", heading_font: "patrick-hand",
      corners: "soft",
      backdrop: { kind: "grid", color: "#f4f1e8", opacity: 0.05, scale: 28 },
      style: { transition: "flip", title_style: "underline", button_fill: "flat", confetti_emoji: "✏️📚🍎" },
    },
  },
];
