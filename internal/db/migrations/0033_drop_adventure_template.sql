-- Removes the is_template flag introduced in 0031. The starter-
-- scaffold flow it enabled (a single bundled "Team-building tour"
-- admin-owned adventure that arrangers cloned via "Use this
-- template") didn't pull its weight: the scaffolded content was
-- generic enough that customising it took as much work as
-- authoring from scratch, and the parallel concept ("template" as
-- a starter you copy vs. "catalog adventure" you license) confused
-- the model arrangers were trying to learn.
--
-- After this migration, admin-owned global-catalog adventures are
-- visible to org users only when `published = 1` — the same gate
-- everyone else respects. The duplicate endpoint stays available
-- (you can still clone your own org's adventures or a published
-- catalog row), it just no longer keys off a flag that no longer
-- exists.

DROP INDEX IF EXISTS idx_adventures_template;
ALTER TABLE adventures DROP COLUMN is_template;
