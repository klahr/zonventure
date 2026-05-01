-- Adventure templates: starter content arrangers can duplicate into
-- their own org instead of authoring from scratch. The activation
-- killer for non-technical buyers (HR, school coordinators) was an
-- empty Adventures tab on first login; templates give them a
-- working starting point they can customise.
--
-- A template is an adventure with org_id IS NULL (admin-owned, in
-- the global catalog) AND is_template = 1. The template flag is
-- distinct from "published" because:
--
--   * templates are visible regardless of publish state — they're
--     intended for use, not browse-and-buy;
--   * a future commercial template may be admin-owned, published,
--     AND not a template (it's a finished adventure organisations
--     license rather than copy);
--   * the "Use this template" UI surfaces only the template-flagged
--     rows, separate from the regular catalog.
--
-- Cloning is transactional and copies pages, hints, accept-lists,
-- images, audio, and the cover avatar. The clone is org-owned and
-- the original stays untouched, so an arranger editing their copy
-- can't break the template for anyone else.
ALTER TABLE adventures ADD COLUMN is_template INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_adventures_template
  ON adventures(is_template)
  WHERE is_template = 1;
