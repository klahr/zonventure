import { WIDE, wideSrcSet } from "@zonventure/ui";

/** The thank-you picture across a card's top (a party-card--cover). */
export function ThankYouBanner() {
  return (
    <img
      className="party-banner"
      src={WIDE.thankYou.small}
      srcSet={wideSrcSet(WIDE.thankYou)}
      sizes="(max-width: 40rem) 100vw, 40rem"
      alt=""
      width={1376}
      height={459}
    />
  );
}

/** A house picture above a card's message: an empty list, a missing page. */
export function Spot({ src }: { src: string }) {
  return <img className="party-spot" src={src} alt="" width={480} height={480} />;
}
