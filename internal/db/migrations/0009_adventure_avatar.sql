-- Per-adventure cover picture, stored inline on the adventures row.
-- Used as a small thumbnail in the store, catalog, library grid, run
-- detail, and dashboard. One image per adventure; clients fall back to
-- a generated initials tile when the blob is absent.
ALTER TABLE adventures ADD COLUMN avatar_mime_type  TEXT;
ALTER TABLE adventures ADD COLUMN avatar_size_bytes INTEGER;
ALTER TABLE adventures ADD COLUMN avatar_content    BLOB;
