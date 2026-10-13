-- Public sign-up starts off on a fresh install: anyone could otherwise
-- create an organisation before the operator has looked. A database
-- that already has users keeps its stored setting; the admin's
-- first-run checklist offers to turn sign-up on.
UPDATE settings SET value = '0'
WHERE key = 'signup_enabled' AND NOT EXISTS (SELECT 1 FROM users);
