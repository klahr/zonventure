-- Auth audit log. One row per security-relevant credential interaction
-- (login, set-password, magic-link exchange, logout) regardless of
-- outcome. Never stores the password. The user_id is nullable so that
-- failed logins for non-existent usernames still leave a row — that's
-- exactly the trace an investigator wants when probing for stuffing.
--
-- Read by the admin Audit tab; admins see all rows, org_admins see
-- only rows for users in their org (rows with user_id NULL stay
-- admin-only since they have no org affinity).

CREATE TABLE auth_events (
    id         BIGSERIAL PRIMARY KEY,
    ts         TEXT    NOT NULL DEFAULT now_text(),
    event      TEXT    NOT NULL,           -- login | set_password | logout | magic_link
    ok         INTEGER NOT NULL,           -- 0/1, matches our existing convention
    user_id    BIGINT  REFERENCES users(id) ON DELETE SET NULL,
    username   TEXT    NOT NULL DEFAULT '',
    ip         TEXT    NOT NULL DEFAULT '',
    user_agent TEXT    NOT NULL DEFAULT '',
    reason     TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_auth_events_ts   ON auth_events(ts DESC);
CREATE INDEX idx_auth_events_user ON auth_events(user_id, ts DESC);
