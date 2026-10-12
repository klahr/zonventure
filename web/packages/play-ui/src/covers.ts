/**
 * A starter theme's cover picture (STARTER_THEMES id), 16:9: small (800
 * px wide, a card's backdrop) and wide (1600 px, a page's banner). Its
 * subject is on the right, its left side calm, for text. Every theme the
 * party adventure creator offers has one (made with
 * `make theme-cover`). Print takes its own copy: internal/trail/covers.
 */
export interface ThemeCover {
  small: string;
  wide: string;
}

export const THEME_COVERS: Record<string, ThemeCover> = {
  unicorn: {
    small: new URL("./covers/unicorn-800.webp", import.meta.url).href,
    wide: new URL("./covers/unicorn-1600.webp", import.meta.url).href,
  },
  autumn: {
    small: new URL("./covers/autumn-800.webp", import.meta.url).href,
    wide: new URL("./covers/autumn-1600.webp", import.meta.url).href,
  },
  "back-to-school": {
    small: new URL("./covers/back-to-school-800.webp", import.meta.url).href,
    wide: new URL("./covers/back-to-school-1600.webp", import.meta.url).href,
  },
  candy: {
    small: new URL("./covers/candy-800.webp", import.meta.url).href,
    wide: new URL("./covers/candy-1600.webp", import.meta.url).href,
  },
  christmas: {
    small: new URL("./covers/christmas-800.webp", import.meta.url).href,
    wide: new URL("./covers/christmas-1600.webp", import.meta.url).href,
  },
  crayfish: {
    small: new URL("./covers/crayfish-800.webp", import.meta.url).href,
    wide: new URL("./covers/crayfish-1600.webp", import.meta.url).href,
  },
  detective: {
    small: new URL("./covers/detective-800.webp", import.meta.url).href,
    wide: new URL("./covers/detective-1600.webp", import.meta.url).href,
  },
  easter: {
    small: new URL("./covers/easter-800.webp", import.meta.url).href,
    wide: new URL("./covers/easter-1600.webp", import.meta.url).href,
  },
  halloween: {
    small: new URL("./covers/halloween-800.webp", import.meta.url).href,
    wide: new URL("./covers/halloween-1600.webp", import.meta.url).href,
  },
  "ice-cream": {
    small: new URL("./covers/ice-cream-800.webp", import.meta.url).href,
    wide: new URL("./covers/ice-cream-1600.webp", import.meta.url).href,
  },
  jungle: {
    small: new URL("./covers/jungle-800.webp", import.meta.url).href,
    wide: new URL("./covers/jungle-1600.webp", import.meta.url).href,
  },
  lucia: {
    small: new URL("./covers/lucia-800.webp", import.meta.url).href,
    wide: new URL("./covers/lucia-1600.webp", import.meta.url).href,
  },
  midsummer: {
    small: new URL("./covers/midsummer-800.webp", import.meta.url).href,
    wide: new URL("./covers/midsummer-1600.webp", import.meta.url).href,
  },
  "new-year": {
    small: new URL("./covers/new-year-800.webp", import.meta.url).href,
    wide: new URL("./covers/new-year-1600.webp", import.meta.url).href,
  },
  ocean: {
    small: new URL("./covers/ocean-800.webp", import.meta.url).href,
    wide: new URL("./covers/ocean-1600.webp", import.meta.url).href,
  },
  office: {
    small: new URL("./covers/office-800.webp", import.meta.url).href,
    wide: new URL("./covers/office-1600.webp", import.meta.url).href,
  },
  "pen-and-paper": {
    small: new URL("./covers/pen-and-paper-800.webp", import.meta.url).href,
    wide: new URL("./covers/pen-and-paper-1600.webp", import.meta.url).href,
  },
  space: {
    small: new URL("./covers/space-800.webp", import.meta.url).href,
    wide: new URL("./covers/space-1600.webp", import.meta.url).href,
  },
  party: {
    small: new URL("./covers/party-800.webp", import.meta.url).href,
    wide: new URL("./covers/party-1600.webp", import.meta.url).href,
  },
  pirate: {
    small: new URL("./covers/pirate-800.webp", import.meta.url).href,
    wide: new URL("./covers/pirate-1600.webp", import.meta.url).href,
  },
  rainbow: {
    small: new URL("./covers/rainbow-800.webp", import.meta.url).href,
    wide: new URL("./covers/rainbow-1600.webp", import.meta.url).href,
  },
  "retro-80s": {
    small: new URL("./covers/retro-80s-800.webp", import.meta.url).href,
    wide: new URL("./covers/retro-80s-1600.webp", import.meta.url).href,
  },
  spring: {
    small: new URL("./covers/spring-800.webp", import.meta.url).href,
    wide: new URL("./covers/spring-1600.webp", import.meta.url).href,
  },
  summer: {
    small: new URL("./covers/summer-800.webp", import.meta.url).href,
    wide: new URL("./covers/summer-1600.webp", import.meta.url).href,
  },
  valentines: {
    small: new URL("./covers/valentines-800.webp", import.meta.url).href,
    wide: new URL("./covers/valentines-1600.webp", import.meta.url).href,
  },
  walpurgis: {
    small: new URL("./covers/walpurgis-800.webp", import.meta.url).href,
    wide: new URL("./covers/walpurgis-1600.webp", import.meta.url).href,
  },
  winter: {
    small: new URL("./covers/winter-800.webp", import.meta.url).href,
    wide: new URL("./covers/winter-1600.webp", import.meta.url).href,
  },
  sunshine: {
    small: new URL("./covers/sunshine-800.webp", import.meta.url).href,
    wide: new URL("./covers/sunshine-1600.webp", import.meta.url).href,
  },
};

/**
 * The cover to show: the look's own picture (url, e.g. /media/{id}) when
 * it has one, else its starter theme's.
 */
export function coverOf(theme: string | undefined, url?: string): ThemeCover | undefined {
  if (url) return { small: url, wide: url };
  return THEME_COVERS[theme ?? ""];
}
