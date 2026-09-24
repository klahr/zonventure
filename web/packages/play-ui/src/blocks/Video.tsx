import { play } from "@zonventure/api";
import type { Renderer } from "./context.ts";

/** A film, fetched only when played: videos are never prefetched. */
export const VideoBlock: Renderer<"video"> = ({ block }) => (
  <figure className="pv-video">
    <video controls playsInline preload="metadata" src={play.mediaURL(block.media)} aria-label={block.alt || undefined} />
    {block.alt && <figcaption>{block.alt}</figcaption>}
  </figure>
);
