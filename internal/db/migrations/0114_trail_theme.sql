-- The starter theme a party adventure's look was taken from (play-ui's
-- STARTER_THEMES id), for the creator to show as chosen. The look itself
-- is the adventure's own colours, fonts and backdrop, as for any
-- adventure; empty is the default look.
ALTER TABLE adventures ADD COLUMN trail_theme TEXT NOT NULL DEFAULT '';
