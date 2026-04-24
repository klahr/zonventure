-- Per-adventure copy for the first-run welcome screen (intro) and the
-- end-of-adventure stats screen (outro). Both are plain text (multi-
-- line), shown as-is to the player. Empty string keeps the built-in
-- default copy so existing adventures aren't broken.
ALTER TABLE adventures ADD COLUMN intro_text TEXT NOT NULL DEFAULT '';
ALTER TABLE adventures ADD COLUMN outro_text TEXT NOT NULL DEFAULT '';
