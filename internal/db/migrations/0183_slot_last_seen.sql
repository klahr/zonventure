-- When a player's phone was last heard from: any request of the player
-- app, or its live connection staying open, stamped at most every 30
-- seconds (auth.SeenSlot). A phone that sends no location still counts
-- as there while its app is open, so its group is not taken for one
-- whose players are all away (play.away). UTC text, as last_login_at.
ALTER TABLE player_slots ADD COLUMN last_seen_at TEXT;
