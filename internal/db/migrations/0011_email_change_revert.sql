-- Email-change revert window. After ConfirmEmailChange flips the
-- email, we stash the previous one + a timestamp so the user can
-- "undo" within a grace window if they realise they typed the new
-- address wrong. NULL means no recent change to revert.
ALTER TABLE users ADD COLUMN previous_email        TEXT;
ALTER TABLE users ADD COLUMN previous_email_set_at TEXT;
