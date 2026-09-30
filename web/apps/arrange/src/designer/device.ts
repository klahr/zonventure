/** A phone-sized screen, where the designer opens on the outline and steps on their simple form. */
export const NARROW = "(max-width: 720px)";

export function isNarrow(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia?.(NARROW).matches;
}

/** A touch screen, where the camera and the phone's position are at hand. */
export function isTouch(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia?.("(pointer: coarse)").matches;
}
