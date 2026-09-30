-- The cheat password is kept as it was typed, so the author can see it
-- and tell testers; it unlocks nothing but cheating in that adventure.
-- 0080 stored a hash, which cannot be shown, so any set is cleared and
-- has to be entered again.
ALTER TABLE adventures RENAME COLUMN cheat_password_hash TO cheat_password;
UPDATE adventures SET cheat_password = NULL WHERE cheat_password IS NOT NULL;
