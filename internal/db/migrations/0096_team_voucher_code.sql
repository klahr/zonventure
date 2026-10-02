-- A short, human-typeable code that gets a player into a team the same
-- way the team's magic link does — a "voucher" to enter at /play, handy
-- for gift cards and emailed gift-box redemptions where typing beats
-- clicking. It is a shorter alias for teams.token: /play resolves the
-- code to the team and sends the player to its /team/{token} page.
--
-- Stored normalised: six upper-case alphanumeric characters (A-Z, 0-9),
-- no separators. The partial unique index keeps live codes unique while
-- letting a NULL (a team that never got one) sit freely. Existing teams
-- are backfilled with a generated code so every team is reachable by
-- voucher.
ALTER TABLE teams ADD COLUMN voucher_code TEXT;

-- Backfill: six upper-case hex chars (0-9A-F — a subset of the
-- alphanumeric format) from a per-row md5, so every existing team gets a
-- distinct code. Mixing the row id into the hash makes a duplicate
-- unlikely; the unique index below is the backstop.
UPDATE teams
   SET voucher_code = upper(substr(md5(random()::text || ':' || id::text), 1, 6))
 WHERE voucher_code IS NULL;

CREATE UNIQUE INDEX idx_teams_voucher_code
    ON teams(voucher_code)
    WHERE voucher_code IS NOT NULL;
