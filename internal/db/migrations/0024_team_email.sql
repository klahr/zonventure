-- Per-team contact email. The arranger sets this for ops use:
-- knowing which captain to message when the team's running late,
-- or auto-sending the recap link to the captain when the event
-- completes. Players don't see this field on the team-share page;
-- it's strictly arranger-side metadata.
--
-- Per-event by design — same physical group playing two events
-- is two unrelated team rows (the existing model). A persistent
-- "org_teams" identity that survives across events is a real
-- refactor and tracked separately; this migration is the cheap
-- per-event variant.
--
-- Default '' rather than NULL so the SELECT path doesn't have to
-- juggle sql.NullString — most callers want an empty string when
-- absent, not a maybe-string.
ALTER TABLE teams ADD COLUMN email TEXT NOT NULL DEFAULT '';

-- Partial index on non-empty values only. Keeps the index small
-- for orgs that don't fill emails on most teams, and supports the
-- future "find by email" lookup in the cross-event teams view
-- (#2 in the product roadmap) without scanning the whole table.
CREATE INDEX idx_teams_email
    ON teams(email) WHERE email <> '';
