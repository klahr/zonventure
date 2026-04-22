-- Organizations are the tenant. Admins own the global tier and create
-- organizations; org_admin/org_member work inside one.
CREATE TABLE organizations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Users are only admins and org staff. Players are not users — they are
-- anonymous slot rows on a team, reached by magic link.
CREATE TABLE users (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    username           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash      TEXT,
    role               TEXT NOT NULL CHECK (role IN ('admin','org_admin','org_member')),
    org_id             INTEGER REFERENCES organizations(id) ON DELETE CASCADE,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    CHECK (
        (role = 'admin'     AND org_id IS NULL) OR
        (role IN ('org_admin','org_member') AND org_id IS NOT NULL)
    )
);

CREATE INDEX idx_users_org ON users(org_id);
CREATE INDEX idx_users_created_by ON users(created_by_user_id);

-- Staff sessions (web logins for admins and org users).
CREATE TABLE sessions (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions(user_id);

-- Adventures live in the database now (admins author them via the editor).
CREATE TABLE adventures (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    slug        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    author      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE adventure_pages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    adventure_id INTEGER NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    idx          INTEGER NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    prompt       TEXT NOT NULL DEFAULT '',
    hint         TEXT NOT NULL DEFAULT '',
    accept       TEXT NOT NULL DEFAULT '[]',   -- JSON array of accepted answers
    lat          REAL NOT NULL DEFAULT 0,
    lng          REAL NOT NULL DEFAULT 0,
    radius_m     REAL,                          -- NULL = not gated
    UNIQUE(adventure_id, idx)
);

CREATE INDEX idx_pages_adventure ON adventure_pages(adventure_id, idx);

CREATE TABLE adventure_page_images (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    page_id     INTEGER NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    content     BLOB NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_page_images ON adventure_page_images(page_id, idx);

-- One-shot licences granted by admins to an organization.
CREATE TABLE adventure_assignments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id          INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    adventure_slug  TEXT NOT NULL,
    max_teams       INTEGER NOT NULL CHECK (max_teams > 0),
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_assignments_org ON adventure_assignments(org_id);

CREATE TABLE adventure_runs (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    adventure_slug     TEXT NOT NULL,
    org_id             INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    state              TEXT NOT NULL CHECK (state IN ('scheduled','running','paused','completed')),
    scheduled_at       TEXT,
    started_at         TEXT,
    paused_at          TEXT,
    completed_at       TEXT,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    assignment_id      INTEGER REFERENCES adventure_assignments(id),
    live_token         TEXT NOT NULL UNIQUE,            -- opaque URL segment for /live/{token}
    show_map           INTEGER NOT NULL DEFAULT 0       -- 0/1; whether the public page shows the real-time map
);

CREATE INDEX idx_runs_state ON adventure_runs(state);
CREATE INDEX idx_runs_org ON adventure_runs(org_id);
CREATE UNIQUE INDEX idx_runs_assignment ON adventure_runs(assignment_id) WHERE assignment_id IS NOT NULL;

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

-- Players are per-slot rows on a team, each with its own unguessable token
-- delivered to the individual (magic-link login). A slot belongs to
-- exactly one team, and via the team to exactly one run.
CREATE TABLE player_slots (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id       INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token         TEXT NOT NULL UNIQUE,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    last_login_at TEXT,
    UNIQUE(team_id, name)
);

CREATE INDEX idx_slots_team ON player_slots(team_id);

-- Per-slot session cookies. Distinct from staff sessions so the two
-- audiences never accidentally share credentials.
CREATE TABLE slot_sessions (
    token       TEXT PRIMARY KEY,
    slot_id     INTEGER NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_slot_sessions_slot ON slot_sessions(slot_id);

CREATE TABLE location_pings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slot_id    INTEGER NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
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

CREATE INDEX idx_pings_run_ts  ON location_pings(run_id, server_ts);
CREATE INDEX idx_pings_team_ts ON location_pings(team_id, server_ts);

CREATE TABLE answer_attempts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id       INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slot_id       INTEGER NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    page_index    INTEGER NOT NULL,
    answer        TEXT NOT NULL,
    accepted      INTEGER NOT NULL,
    submitted_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_attempts_team ON answer_attempts(team_id, submitted_at);

-- Run events feed. actor_user_id is set for staff actions, actor_slot_id
-- for slot actions; at most one of them is non-NULL.
CREATE TABLE run_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    team_id        INTEGER REFERENCES teams(id) ON DELETE SET NULL,
    actor_user_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    actor_slot_id  INTEGER REFERENCES player_slots(id) ON DELETE SET NULL,
    kind           TEXT NOT NULL,
    payload        TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_run_events_run_ts ON run_events(run_id, created_at);
