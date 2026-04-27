-- Operator-tunable settings. A single key/value table rather than a
-- column per setting so adding the next admin toggle (e.g. require-
-- email-verify-before-login, default-org-quota) is a one-line INSERT
-- instead of a schema migration.
--
-- value is TEXT to keep the table generic; callers cast to bool/int
-- in Go. updated_at is purely diagnostic — useful when an operator
-- asks "when did signup get turned off?" and the audit log is full.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT now_text()
);

-- Default: open signup, matching the historical behaviour. Self-host
-- operators flip this off from the admin Settings page when they want
-- a closed deployment (e.g. a single-org install with members invited
-- by the org admin).
INSERT INTO settings(key, value) VALUES ('signup_enabled', '1');
