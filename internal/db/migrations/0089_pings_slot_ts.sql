-- The anti-teleport rule and the zone conditions read each phone's
-- latest pings, not the team's: one slot's, newest first.
CREATE INDEX IF NOT EXISTS idx_pings_slot_ts ON location_pings(slot_id, server_ts);
DROP INDEX IF EXISTS idx_pings_slot;
