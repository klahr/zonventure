-- The name a person gives at sign-up or in Settings → Account, so the
-- console greets them by it instead of by their email. Empty or NULL
-- is "no name": the greeting goes without one.
ALTER TABLE users ADD COLUMN display_name TEXT;
