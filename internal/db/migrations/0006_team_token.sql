-- Per-team share token. Drives the public /team/{token} page where an
-- arranger sends ONE URL to the team captain and each member picks
-- their own magic link from there, instead of the arranger copying
-- N×slot links by hand.
--
-- The column is NOT NULL with a temporary default of '' so the ALTER
-- is accepted on existing rows; we immediately backfill with
-- randomblob-derived tokens so every team has a distinct unguessable
-- value. Fresh inserts in CreateTeam will overwrite the default with
-- a properly-generated token.
ALTER TABLE teams ADD COLUMN token TEXT NOT NULL DEFAULT '';

-- hex(randomblob(18)) → 36 lowercase hex chars ~= 144 bits of entropy,
-- which is safely unguessable and copy-pasteable.
UPDATE teams SET token = lower(hex(randomblob(18))) WHERE token = '';

CREATE UNIQUE INDEX idx_teams_token ON teams(token);
