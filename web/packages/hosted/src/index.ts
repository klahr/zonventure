export * from "./api.ts";
export { HostedProvider, useHosted, type Hosted } from "./context.tsx";
export { PlayerLink } from "./PlayerLink.tsx";
export { Game } from "./Game.tsx";
export { TrailPrint } from "./Print.tsx";
export { WhereSection, PersonaliseSection, PlacesSection } from "./Adapt.tsx";
import sv from "./sv.json" with { type: "json" };
/** The package's Swedish strings, to merge into the app's sv catalog. */
export const hostedSv: Record<string, string> = sv;
