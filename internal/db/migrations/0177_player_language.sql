-- The language a player chose for their own phone; empty follows the team's.
ALTER TABLE player_slots ADD COLUMN lang TEXT NOT NULL DEFAULT '';
