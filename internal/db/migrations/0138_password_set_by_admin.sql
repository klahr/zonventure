-- Set when an admin chose the password (a new org user, an admin
-- reset); cleared when the user sets their own. The console asks the
-- user to replace it.
ALTER TABLE users ADD COLUMN password_set_by_admin INTEGER NOT NULL DEFAULT 0;
