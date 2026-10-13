-- Who changed what through the global-admin API: one row per
-- successful state-changing request, shown under Settings → Audit.
CREATE TABLE admin_actions (
    id       BIGSERIAL PRIMARY KEY,
    ts       TEXT   NOT NULL DEFAULT now_text(),
    user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    username TEXT   NOT NULL DEFAULT '',
    action   TEXT   NOT NULL,           -- method + route pattern
    target   TEXT   NOT NULL DEFAULT '' -- request path, with the ids
);

CREATE INDEX idx_admin_actions_ts ON admin_actions(ts DESC);
