-- Adventures start as drafts and need an explicit publish to appear
-- in the arranger-facing store. Existing rows default to published so
-- nothing disappears from running systems at migration time; new
-- rows default to unpublished (the CreateAdventure handler is where
-- the default-for-new-rows policy lives — see admin/authoring.go).
ALTER TABLE adventures ADD COLUMN published INTEGER NOT NULL DEFAULT 0;
UPDATE adventures SET published = 1;
