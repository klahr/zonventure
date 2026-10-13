-- When a player turned location sharing off on their phone. Pings from
-- before then no longer place the player, so a "you are here" from the
-- last fix can't let a check-in answer through once the phone has moved
-- on. NULL is never turned off.
ALTER TABLE player_slots ADD COLUMN location_off_at TEXT;
