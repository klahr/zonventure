-- A dev event plays the adventure's draft as it is edited, for an author
-- trying changes on site: its graph is the adventure's dev row in
-- adventure_graphs (version 0), rewritten on every draft save. Never
-- listed among the events, never billed, never ranked.
ALTER TABLE events ADD COLUMN dev BOOLEAN NOT NULL DEFAULT FALSE;
