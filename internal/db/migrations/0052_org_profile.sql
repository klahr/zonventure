-- Per-organisation public profile fields.
--
-- Surfaced on the per-org landing page (/o/{slug}) and the per-org
-- store (/o/{slug}/store): the tagline appears as a hero subtitle,
-- the cover image becomes the hero background, and the public_website
-- becomes a "visit our site" link in the chrome.
--
-- Cover-image columns mirror the avatar_* / logo_* shape used
-- elsewhere (see internal/storage/dbblob/dbblob.go) so the same
-- read path serves /org-cover/{id} the same way /org-logo/{id} is
-- served. Inline bytes-in-Postgres rationale matches 0030:
-- self-host expects one binary + one Postgres, no S3; covers
-- re-encoded server-side to a small ceiling; pg_dump stays
-- self-contained.

ALTER TABLE organizations ADD COLUMN tagline TEXT;
ALTER TABLE organizations ADD COLUMN public_website TEXT;
ALTER TABLE organizations ADD COLUMN cover_image_mime_type   TEXT;
ALTER TABLE organizations ADD COLUMN cover_image_size_bytes  INTEGER;
ALTER TABLE organizations ADD COLUMN cover_image_content     BYTEA;
