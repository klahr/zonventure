import type { play } from "@zonventure/api";
import type { BlockContext } from "./context.ts";

/** A block whose condition does not hold yet: that something is here, and where to go when that is the condition. */
export function Teaser({ block, ctx }: { block: play.PlayBlock; ctx: BlockContext }) {
  const { t } = ctx;
  return (
    <div className="pv-teaser" aria-live="polite">
      <span className="pv-teaser__lock" aria-hidden="true">
        🔒
      </span>
      <span className="pv-teaser__text">
        {block.place ? t("Get closer to {{place}} to reveal this.", { place: block.place.name || block.place.slug }) : t("Something is hidden here. It appears when the time is right.")}
      </span>
      {block.place && ctx.placeNote && <span className="pv-teaser__note">{ctx.placeNote(block.place)}</span>}
    </div>
  );
}
