-- Each player account belongs to the arranger that created it, so players
-- are not shared across arrangers. Pre-existing rows get NULL and become
-- invisible to arrangers (root still sees them) — that's acceptable because
-- we are pre-release and the only data set up via seed-demo will be
-- reinstalled.
ALTER TABLE users ADD COLUMN created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_users_created_by ON users(created_by_user_id);
