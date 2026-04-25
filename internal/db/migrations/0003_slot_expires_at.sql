-- Magic-link tokens (player_slots.token) gain an explicit expiry. NULL
-- preserves the legacy "never expires" behaviour for rows created before
-- this migration; slots created via the admin endpoints from now on get
-- a computed default (run.scheduled_at + 7 days, or now + 30 days when
-- the run is unscheduled) so a forgotten link auto-stales after the
-- adventure is over instead of lingering forever.
--
-- The slot's last_login_at column is reused as "last used at" — same
-- semantic, no need for a duplicate column.

ALTER TABLE player_slots ADD COLUMN expires_at TEXT;
