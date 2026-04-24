-- Per-team profile picture, stored inline on the teams row. Teams have
-- exactly one avatar so there's no point in a side-table. Any team
-- without a blob falls back to an initials-on-colour avatar generated
-- on the client.
ALTER TABLE teams ADD COLUMN avatar_mime_type  TEXT;
ALTER TABLE teams ADD COLUMN avatar_size_bytes INTEGER;
ALTER TABLE teams ADD COLUMN avatar_content    BLOB;
