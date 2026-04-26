-- Per-session metadata for the multi-device session UI in account
-- settings. Lets a user see "Phone Safari from 192.0.2.5, last used
-- 3 minutes ago" and revoke individual sessions. None of the columns
-- are required for authentication itself — they're surface state
-- only — so all three are NULLable + populated best-effort.
ALTER TABLE sessions ADD COLUMN user_agent   TEXT;
ALTER TABLE sessions ADD COLUMN ip           TEXT;
ALTER TABLE sessions ADD COLUMN last_used_at TEXT;
