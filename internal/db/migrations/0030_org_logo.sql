-- Per-organisation brand logo. Surfaced today on the recap PDF
-- cover page (replacing the text-only org name when set); future
-- iterations can drop the same image into the play page header
-- and the arranger sidebar.
--
-- Inline columns mirror the avatar_* shape on teams and adventures
-- (see internal/storage/dbblob/dbblob.go) so the same dbblob path
-- handles all three. Bytes-in-Postgres for v1 because:
--   1) self-host expects one binary + one Postgres, no S3,
--   2) logos are small (we cap at ~200 KB after re-encoding),
--   3) backups via pg_dump remain self-contained.
ALTER TABLE organizations ADD COLUMN logo_mime_type   TEXT;
ALTER TABLE organizations ADD COLUMN logo_size_bytes  INTEGER;
ALTER TABLE organizations ADD COLUMN logo_content     BYTEA;
