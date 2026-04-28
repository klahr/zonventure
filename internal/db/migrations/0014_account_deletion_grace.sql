-- Account-deletion grace period. DeleteMyAccount no longer cascades
-- the row immediately; instead it sets scheduled_deletion_at to "now
-- + grace window" and returns. The retention sweeper hard-deletes
-- once the timestamp passes. Until then the user can log in and call
-- /api/admin/me/cancel-deletion to clear the field.
ALTER TABLE users ADD COLUMN scheduled_deletion_at TIMESTAMPTZ;

-- Partial index — most users won't have this set, and the sweeper
-- queries for "scheduled_deletion_at <= now()" so the index only needs
-- to cover the rows that are actually pending.
CREATE INDEX users_scheduled_deletion_at_idx
  ON users (scheduled_deletion_at)
  WHERE scheduled_deletion_at IS NOT NULL;
