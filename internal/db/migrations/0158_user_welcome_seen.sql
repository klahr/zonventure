-- The console's welcome dialog was shown to this person, on whichever
-- device: it is not shown again on another browser.
ALTER TABLE users ADD COLUMN welcome_seen BOOLEAN NOT NULL DEFAULT FALSE;
