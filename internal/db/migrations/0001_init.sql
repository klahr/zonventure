CREATE TABLE users (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('player','arranger','root')),
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE arranger_adventures (
    arranger_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    adventure_slug   TEXT NOT NULL,
    granted_at       TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (arranger_user_id, adventure_slug)
);

CREATE TABLE sessions (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE adventure_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    adventure_slug   TEXT NOT NULL,
    arranger_user_id INTEGER NOT NULL REFERENCES users(id),
    state            TEXT NOT NULL CHECK (state IN ('scheduled','running','paused','completed')),
    scheduled_at     TEXT,
    started_at       TEXT,
    paused_at        TEXT,
    completed_at     TEXT,
    created_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_runs_state ON adventure_runs(state);

CREATE TABLE teams (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id              INTEGER NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    current_page_index  INTEGER NOT NULL DEFAULT 0,
    completed_at        TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(run_id, name)
);

CREATE INDEX idx_teams_run ON teams(run_id);

CREATE TABLE team_members (
    team_id  INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX idx_team_members_user ON team_members(user_id);

CREATE TABLE location_pings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    run_id     INTEGER NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    lat        REAL NOT NULL,
    lng        REAL NOT NULL,
    accuracy   REAL,
    speed      REAL,
    heading    REAL,
    client_ts  TEXT,
    server_ts  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_pings_run_ts ON location_pings(run_id, server_ts);
CREATE INDEX idx_pings_team_ts ON location_pings(team_id, server_ts);

CREATE TABLE answer_attempts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id       INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    page_index    INTEGER NOT NULL,
    answer        TEXT NOT NULL,
    accepted      INTEGER NOT NULL,
    submitted_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_attempts_team ON answer_attempts(team_id, submitted_at);
