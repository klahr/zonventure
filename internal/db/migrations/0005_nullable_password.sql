-- Players now set their own password on first login, so accounts are
-- created without one. SQLite needs a table rebuild to relax the NOT NULL
-- constraint; defer_foreign_keys keeps the FKs from other tables happy
-- during the DROP/RENAME dance.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE users_new (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    username           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash      TEXT,
    role               TEXT NOT NULL CHECK (role IN ('player','arranger','root')),
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL
);

INSERT INTO users_new(id, username, password_hash, role, created_at, created_by_user_id)
SELECT id, username, password_hash, role, created_at, created_by_user_id FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

CREATE INDEX idx_users_created_by ON users(created_by_user_id);
