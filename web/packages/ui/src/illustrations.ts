/**
 * Zonventure's own pictures for its screens: the welcome dialog, sign-in,
 * empty lists, errors and thank-yous (made with
 * `make illustration`). Each carries its own background, so it reads as a
 * picture in a rounded frame on light and dark alike.
 *
 * Spots are square (480 px), shown at 8–10rem above a message. Wide
 * pictures come small (800 px) and wide (the source's width, at most
 * 1600 px): banners are cropped to 3:1 by the page, scenes shown 4:3.
 */
export interface WidePicture {
  small: string;
  wide: string;
  /** The wide file's width in pixels, for srcSet. */
  width: number;
}

export const SPOTS = {
  checkEmail: new URL("./illustrations/check-email.webp", import.meta.url).href,
  invitation: new URL("./illustrations/invitation.webp", import.meta.url).href,
  noAdventures: new URL("./illustrations/no-adventures.webp", import.meta.url).href,
  noEvents: new URL("./illustrations/no-events.webp", import.meta.url).href,
  noPartyOffers: new URL("./illustrations/no-party-offers.webp", import.meta.url).href,
  noPurchases: new URL("./illustrations/no-purchases.webp", import.meta.url).href,
  noRedemptions: new URL("./illustrations/no-redemptions.webp", import.meta.url).href,
  noSelfGuided: new URL("./illustrations/no-self-guided.webp", import.meta.url).href,
  noTeams: new URL("./illustrations/no-teams.webp", import.meta.url).href,
  noTrails: new URL("./illustrations/no-trails.webp", import.meta.url).href,
  notFound: new URL("./illustrations/not-found.webp", import.meta.url).href,
  published: new URL("./illustrations/published.webp", import.meta.url).href,
  wentWrong: new URL("./illustrations/went-wrong.webp", import.meta.url).href,
} satisfies Record<string, string>;

export const WIDE = {
  welcome: {
    small: new URL("./illustrations/welcome-800.webp", import.meta.url).href,
    wide: new URL("./illustrations/welcome-wide.webp", import.meta.url).href,
    width: 1376,
  },
  signIn: {
    small: new URL("./illustrations/sign-in-800.webp", import.meta.url).href,
    wide: new URL("./illustrations/sign-in-wide.webp", import.meta.url).href,
    width: 1088,
  },
  noOrganisation: {
    small: new URL("./illustrations/no-organisation-800.webp", import.meta.url).href,
    wide: new URL("./illustrations/no-organisation-wide.webp", import.meta.url).href,
    width: 978,
  },
  thankYou: {
    small: new URL("./illustrations/thank-you-800.webp", import.meta.url).href,
    wide: new URL("./illustrations/thank-you-wide.webp", import.meta.url).href,
    width: 1376,
  },
} satisfies Record<string, WidePicture>;

/** srcSet for a wide picture, for an <img> with sizes. */
export const wideSrcSet = (p: WidePicture) => `${p.small} 800w, ${p.wide} ${p.width}w`;
