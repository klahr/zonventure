-- When an event ends, its players' sessions are marked ended instead of
-- deleted: the game is over, but a player can still erase their data or
-- sign out from the phone they played on.
ALTER TABLE slot_sessions ADD COLUMN ended BOOLEAN NOT NULL DEFAULT false;
