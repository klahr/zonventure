-- The .ics SEQUENCE: bumped whenever the event's time changes or it is
-- cancelled, so calendar apps replace the entry they already hold.
ALTER TABLE events ADD COLUMN ics_sequence INTEGER NOT NULL DEFAULT 0;
