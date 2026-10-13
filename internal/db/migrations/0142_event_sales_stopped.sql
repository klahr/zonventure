-- A self-guided event whose organiser stopped selling tickets: it leaves
-- the store and checkout refuses it, while teams that already have a
-- ticket play on. Pausing still freezes play, for live emergencies.
ALTER TABLE events ADD COLUMN sales_stopped BOOLEAN NOT NULL DEFAULT FALSE;
