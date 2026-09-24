// Distances and directions on the ground, for the compass and the map.

/** Great-circle distance in metres, for "about 120 m away". */
export function metresBetween(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const r = 6371000;
  const rad = (d: number) => (d * Math.PI) / 180;
  const dLat = rad(lat2 - lat1);
  const dLng = rad(lng2 - lng1);
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(rad(lat1)) * Math.cos(rad(lat2)) * Math.sin(dLng / 2) ** 2;
  return r * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

/** The initial compass bearing from one point to another: 0 is north, 90 east. */
export function bearing(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const rad = (d: number) => (d * Math.PI) / 180;
  const y = Math.sin(rad(lng2 - lng1)) * Math.cos(rad(lat2));
  const x = Math.cos(rad(lat1)) * Math.sin(rad(lat2)) - Math.sin(rad(lat1)) * Math.cos(rad(lat2)) * Math.cos(rad(lng2 - lng1));
  return ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360;
}

/**
 * The angle to turn a needle to from `prev` to show `next` without
 * spinning the long way round: 350° → 10° turns 20°, not back 340°.
 */
export function nearestTurn(prev: number, next: number): number {
  const d = ((((next - prev) % 360) + 540) % 360) - 180;
  return prev + d;
}
