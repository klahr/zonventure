-- Per-event cover image. Surfaces on /store cards, on the event
-- detail panel in /arrange, and (eventually) on the player welcome
-- overlay so a buyer recognises the adventure they bought a ticket
-- for. Inline-on-row pattern same as team avatars + adventure
-- avatars (see migrations 0001 + 0019); the storage.Blobs seam
-- abstracts the location so a future S3 / local-FS impl swaps the
-- backing store without touching call sites.
--
-- All three columns are nullable. NULL = no cover (the store card
-- falls back to a text-only layout). MIME is whatever the upload
-- handler decided after running the image through imageutil.Shrink,
-- so it's effectively jpeg / png / webp.
ALTER TABLE events
    ADD COLUMN cover_image_mime_type  TEXT,
    ADD COLUMN cover_image_size_bytes BIGINT,
    ADD COLUMN cover_image_content    BYTEA;
