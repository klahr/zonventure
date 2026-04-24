-- Optional free-text run name. "Run #7" is meaningless when an
-- arranger has hosted several — a human-friendly label ("Saturday
-- Andersson party") makes lists legible. Empty means "fall back to
-- Run #{id}".
ALTER TABLE adventure_runs ADD COLUMN name TEXT NOT NULL DEFAULT '';
