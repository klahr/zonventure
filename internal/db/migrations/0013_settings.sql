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

-- Defaults are tuned for self-host: signup open (so a fresh operator
-- can register their first arranger account through the UI), but the
-- marketing landing OFF (a self-host deployment is an app, not a
-- marketing site — unauthenticated visitors land on /arrange).
-- Hosted ops flip landing_enabled on via the admin Settings page
-- after deploying.
INSERT INTO settings(key, value) VALUES
    ('signup_enabled',  '1'),
    ('landing_enabled', '0');
