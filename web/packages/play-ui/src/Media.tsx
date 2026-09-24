import { play } from "@zonventure/api";

/**
 * A step's or hint's pictures and sounds, in the author's order. Each is
 * fetched from /media/{id}, which the server serves only once the team
 * has reached the step or revealed the hint.
 */
export function MediaList({ media }: { media: play.PlayMedia[] | undefined }) {
  if (!media?.length) return null;
  return (
    <div className="pv-media">
      {media.map((m) =>
        m.kind === "image" ? (
          <img key={m.id} className="pv-media__img" src={play.mediaURL(m.id)} alt={m.alt ?? ""} loading="lazy" decoding="async" />
        ) : (
          <figure key={m.id} className="pv-media__audio">
            <audio controls preload="none" src={play.mediaURL(m.id)} aria-label={m.alt || undefined} />
            {m.alt && <figcaption>{m.alt}</figcaption>}
          </figure>
        ),
      )}
    </div>
  );
}

