-- Per-team token for the public post-event recap page (track replay
-- + stats + outro). Distinct from teams.token so leaking the recap
-- URL does NOT grant team-share / join access; the two have very
-- different blast radii.
--
-- Default '' rather than NOT NULL with a generated value because:
--   - Backfilling random per-row in a SQL migration is awkward
--     (Postgres would need either a function or per-row UPDATE in
--     a loop), and self-host migrations should stay declarative.
--   - The recap feature is forward-looking — old teams from before
--     this migration just don't get a recap URL, which is the same
--     as if they'd been deleted by retention. No data loss.
--   - New teams get a token minted at admin.CreateTeam /
--     BulkCreateTeams / ImportTeams via the existing tokenx package
--     (same shape as teams.token).
--
-- The partial UNIQUE index excludes empty-string defaults so old
-- backfilled rows don't all collide on '' — only minted tokens
-- need to be unique with each other.
ALTER TABLE teams ADD COLUMN recap_token TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX idx_teams_recap_token
    ON teams(recap_token) WHERE recap_token <> '';
