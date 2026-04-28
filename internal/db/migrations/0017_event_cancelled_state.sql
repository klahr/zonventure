-- Add 'cancelled' to events.state. The CHECK constraint was defined
-- inline in 0001_init.sql, so its name was auto-generated against the
-- original table name (`adventure_runs_state_check`), and the rename
-- in 0009 did not propagate. Discover the live name from the catalog
-- to avoid hard-coding a guess.
--
-- Cancelled is a terminal state — read-only, like completed, but
-- distinguished so the UI and the activity log can show "cancelled"
-- instead of misleadingly saying the event finished. cancelled_at
-- captures when the cancellation happened on the same TEXT-ISO8601
-- footing as the other lifecycle timestamps.
DO $$
DECLARE
    cn TEXT;
BEGIN
    SELECT conname INTO cn
    FROM pg_constraint
    WHERE conrelid = 'events'::regclass
      AND contype  = 'c'
      AND pg_get_constraintdef(oid) ILIKE '%state%scheduled%running%';
    IF cn IS NULL THEN
        RAISE EXCEPTION 'events.state CHECK constraint not found';
    END IF;
    EXECUTE 'ALTER TABLE events DROP CONSTRAINT ' || quote_ident(cn);
END $$;

ALTER TABLE events ADD CONSTRAINT events_state_check
    CHECK (state IN ('scheduled','running','paused','completed','cancelled'));

ALTER TABLE events ADD COLUMN cancelled_at TEXT;
