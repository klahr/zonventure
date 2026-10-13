-- The start a team's last invitation stated (UTC, as scheduled_at): one
-- sent again after the event moved says the time changed.
ALTER TABLE teams ADD COLUMN invited_start TEXT;
