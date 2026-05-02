-- Pricing reframe: meter per-player, not per-team. The two columns
-- holding the metering cap rename to max_players, and existing values
-- multiply by 4 (typical team size) so customers don't lose entitlement
-- they already paid for.
--
-- Existing assignments with max_teams=10 ("up to 10 teams") become
-- max_players=40 ("up to 40 player slots in the event"). The team-
-- creation gate downstream changes from "count(teams) <= max_teams"
-- to "count(player_slots in event) + new <= max_players".

UPDATE adventure_assignments SET max_teams = max_teams * 4;
UPDATE purchases              SET max_teams = max_teams * 4;

ALTER TABLE adventure_assignments RENAME COLUMN max_teams TO max_players;
ALTER TABLE purchases             RENAME COLUMN max_teams TO max_players;

-- The inline CHECK constraints were auto-named after the old column.
-- Postgres updates the constraint expression on column rename but
-- leaves the constraint name itself with the stale "max_teams"
-- substring. Drop+readd so \d output reads cleanly.
ALTER TABLE adventure_assignments
    DROP CONSTRAINT IF EXISTS adventure_assignments_max_teams_check;
ALTER TABLE adventure_assignments
    ADD CONSTRAINT adventure_assignments_max_players_check
    CHECK (max_players > 0);

ALTER TABLE purchases
    DROP CONSTRAINT IF EXISTS purchases_max_teams_check;
ALTER TABLE purchases
    ADD CONSTRAINT purchases_max_players_check
    CHECK (max_players > 0);
