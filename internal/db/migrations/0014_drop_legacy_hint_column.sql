-- The single-hint `adventure_pages.hint` column has been unread since
-- migration 0013 introduced the multi-hint table + player UI. Authoring
-- paths, egress DTOs, and the play client all route through
-- adventure_page_hints now. Dropping the column closes the loop so a
-- future reader of the schema can't be confused about which field is
-- authoritative.
--
-- Requires sqlite ≥ 3.35 for DROP COLUMN. modernc.org/sqlite in this
-- repo bundles 3.45+, so the simple form is fine.
ALTER TABLE adventure_pages DROP COLUMN hint;
