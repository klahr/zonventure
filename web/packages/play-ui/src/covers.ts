/**
 * A starter theme's cover picture (STARTER_THEMES id), 16:9: small (800
 * px wide, a card's backdrop) and wide (1600 px, a page's banner). Its
 * subject is on the right, its left side calm, for text. Not every theme
 * has one yet. Print takes its own copy: internal/trail/covers.
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
};
