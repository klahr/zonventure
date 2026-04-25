-- Single-file Postgres init for the Zonventure DB. Collapsed from the
-- 14 forward-only sqlite migrations that grew during development; the
-- final shape is the only thing worth carrying forward.
--
-- Conventions:
--   * Surrogate ids are BIGSERIAL, FKs use BIGINT.
--   * Timestamps remain TEXT in 'YYYY-MM-DD HH:MM:SS' UTC form so JSON
--     payloads keep the wire format the front-end already parses. The
--     now_text() helper produces them; defaults call it instead of now().
--   * Booleans (disabled, accepted, show_map, published) stay INTEGER
--     0/1 to keep the Go code unchanged on this PR.
--   * usernames use citext for case-insensitive uniqueness, replacing
--     sqlite's COLLATE NOCASE.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE OR REPLACE FUNCTION now_text() RETURNS TEXT
LANGUAGE SQL STABLE AS $$
    SELECT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
$$;

-- Organizations are the tenant. Admins own the global tier and create
-- organizations; org_admin/org_member work inside one.
CREATE TABLE organizations (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT now_text()
);

-- Users are only admins and org staff. Players are not users — they are
-- anonymous slot rows on a team, reached by magic link.
CREATE TABLE users (
    id                 BIGSERIAL PRIMARY KEY,
    username           CITEXT NOT NULL UNIQUE,
    password_hash      TEXT,
    role               TEXT NOT NULL CHECK (role IN ('admin','org_admin','org_member')),
    org_id             BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    created_at         TEXT NOT NULL DEFAULT now_text(),
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
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
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT now_text(),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions(user_id);

-- Adventures live in the database (admins author them via the editor).
-- org_id NULL = global admin-owned catalog visible to everyone; non-NULL
-- = private to that org.
CREATE TABLE adventures (
    id                     BIGSERIAL PRIMARY KEY,
    slug                   TEXT NOT NULL UNIQUE,
    title                  TEXT NOT NULL,
    description            TEXT NOT NULL DEFAULT '',
    author                 TEXT NOT NULL DEFAULT '',
    created_at             TEXT NOT NULL DEFAULT now_text(),
    updated_at             TEXT NOT NULL DEFAULT now_text(),
    accent_color           TEXT,
    accent_secondary_color TEXT,
    background_color       TEXT,
    text_color             TEXT,
    org_id                 BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    avatar_mime_type       TEXT,
    avatar_size_bytes      BIGINT,
    avatar_content         BYTEA,
    published              INTEGER NOT NULL DEFAULT 0,
    intro_text             TEXT NOT NULL DEFAULT '',
    outro_text             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_adventures_org ON adventures(org_id);

-- Library puzzles are pages with adventure_id NULL; attached pages have
-- adventure_id set. UNIQUE(adventure_id, idx) treats NULLs as distinct,
-- so library rows can share any idx.
CREATE TABLE adventure_pages (
    id           BIGSERIAL PRIMARY KEY,
    adventure_id BIGINT REFERENCES adventures(id) ON DELETE CASCADE,
    idx          INTEGER NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    prompt       TEXT NOT NULL DEFAULT '',
    accept       TEXT NOT NULL DEFAULT '[]',   -- JSON array of accepted answers
    lat          DOUBLE PRECISION NOT NULL DEFAULT 0,
    lng          DOUBLE PRECISION NOT NULL DEFAULT 0,
    radius_m     DOUBLE PRECISION,              -- NULL = not gated
    disabled     INTEGER NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT '',
    org_id       BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    UNIQUE(adventure_id, idx)
);

CREATE INDEX idx_pages_adventure     ON adventure_pages(adventure_id, idx);
CREATE INDEX idx_adventure_pages_org ON adventure_pages(org_id);

CREATE TABLE adventure_page_images (
    id          BIGSERIAL PRIMARY KEY,
    page_id     BIGINT NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL,
    content     BYTEA NOT NULL,
    created_at  TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_page_images ON adventure_page_images(page_id, idx);

-- Per-puzzle ordered hints with delay gating. Player UI shows a single
-- "Reveal hint" button that unlocks hints in order; the delay counts
-- from teams.current_page_started_at.
CREATE TABLE adventure_page_hints (
    id            BIGSERIAL PRIMARY KEY,
    page_id       BIGINT  NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    idx           INTEGER NOT NULL,
    text          TEXT    NOT NULL DEFAULT '',
    delay_seconds INTEGER NOT NULL DEFAULT 0,
    UNIQUE(page_id, idx)
);

CREATE INDEX idx_page_hints ON adventure_page_hints(page_id, idx);

-- One-shot licences granted by admins to an organization.
CREATE TABLE adventure_assignments (
    id              BIGSERIAL PRIMARY KEY,
    org_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    adventure_slug  TEXT   NOT NULL,
    max_teams       INTEGER NOT NULL CHECK (max_teams > 0),
    created_at      TEXT   NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_assignments_org ON adventure_assignments(org_id);

CREATE TABLE adventure_runs (
    id                 BIGSERIAL PRIMARY KEY,
    adventure_slug     TEXT NOT NULL,
    org_id             BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    state              TEXT NOT NULL CHECK (state IN ('scheduled','running','paused','completed')),
    scheduled_at       TEXT,
    started_at         TEXT,
    paused_at          TEXT,
    completed_at       TEXT,
    created_at         TEXT NOT NULL DEFAULT now_text(),
    assignment_id      BIGINT REFERENCES adventure_assignments(id),
    live_token         TEXT NOT NULL UNIQUE,
    show_map           INTEGER NOT NULL DEFAULT 0,
    name               TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_runs_state ON adventure_runs(state);
CREATE INDEX idx_runs_org   ON adventure_runs(org_id);
CREATE UNIQUE INDEX idx_runs_assignment
    ON adventure_runs(assignment_id) WHERE assignment_id IS NOT NULL;

CREATE TABLE teams (
    id                       BIGSERIAL PRIMARY KEY,
    run_id                   BIGINT NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    current_page_index       INTEGER NOT NULL DEFAULT 0,
    completed_at             TEXT,
    created_at               TEXT NOT NULL DEFAULT now_text(),
    token                    TEXT NOT NULL UNIQUE,
    avatar_mime_type         TEXT,
    avatar_size_bytes        BIGINT,
    avatar_content           BYTEA,
    current_page_started_at  TEXT,
    UNIQUE(run_id, name)
);

CREATE INDEX idx_teams_run ON teams(run_id);

-- Players are per-slot rows on a team, each with its own unguessable
-- token delivered to the individual (magic-link login). A slot belongs
-- to exactly one team, and via the team to exactly one run.
CREATE TABLE player_slots (
    id            BIGSERIAL PRIMARY KEY,
    team_id       BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token         TEXT NOT NULL UNIQUE,
    created_at    TEXT NOT NULL DEFAULT now_text(),
    last_login_at TEXT,
    UNIQUE(team_id, name)
);

CREATE INDEX idx_slots_team ON player_slots(team_id);

-- Per-slot session cookies. Distinct from staff sessions so the two
-- audiences never accidentally share credentials.
CREATE TABLE slot_sessions (
    token       TEXT PRIMARY KEY,
    slot_id     BIGINT NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT now_text(),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_slot_sessions_slot ON slot_sessions(slot_id);

CREATE TABLE location_pings (
    id         BIGSERIAL PRIMARY KEY,
    slot_id    BIGINT NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    team_id    BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    run_id     BIGINT NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    lat        DOUBLE PRECISION NOT NULL,
    lng        DOUBLE PRECISION NOT NULL,
    accuracy   DOUBLE PRECISION,
    speed      DOUBLE PRECISION,
    heading    DOUBLE PRECISION,
    client_ts  TEXT,
    server_ts  TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_pings_run_ts  ON location_pings(run_id,  server_ts);
CREATE INDEX idx_pings_team_ts ON location_pings(team_id, server_ts);

CREATE TABLE answer_attempts (
    id            BIGSERIAL PRIMARY KEY,
    team_id       BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slot_id       BIGINT NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    page_index    INTEGER NOT NULL,
    answer        TEXT NOT NULL,
    accepted      INTEGER NOT NULL,
    submitted_at  TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_attempts_team ON answer_attempts(team_id, submitted_at);

-- Run events feed. actor_user_id is set for staff actions, actor_slot_id
-- for slot actions; at most one of them is non-NULL.
CREATE TABLE run_events (
    id             BIGSERIAL PRIMARY KEY,
    run_id         BIGINT NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    team_id        BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    actor_user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_slot_id  BIGINT REFERENCES player_slots(id) ON DELETE SET NULL,
    kind           TEXT NOT NULL,
    payload        TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_run_events_run_ts ON run_events(run_id, created_at);

-- Purchases are the revenue mechanism: an arranger buys N team slots
-- for an adventure, and on paid we create a normal adventure_assignment
-- row.
CREATE TABLE purchases (
    id                  BIGSERIAL PRIMARY KEY,
    org_id              BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    buyer_user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    adventure_slug      TEXT NOT NULL,
    max_teams           INTEGER NOT NULL CHECK (max_teams > 0),
    amount_cents        INTEGER NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'USD',
    provider            TEXT NOT NULL DEFAULT 'simulated',
    provider_session_id TEXT,
    state               TEXT NOT NULL CHECK (state IN ('pending','paid','failed','refunded')) DEFAULT 'pending',
    assignment_id       BIGINT REFERENCES adventure_assignments(id) ON DELETE SET NULL,
    created_at          TEXT NOT NULL DEFAULT now_text(),
    paid_at             TEXT
);

CREATE INDEX idx_purchases_org   ON purchases(org_id, created_at);
CREATE INDEX idx_purchases_state ON purchases(state);

-- Targeted-share mechanism: admins can grant a specific org the right
-- to schedule runs of a private adventure without going through the
-- public store.
CREATE TABLE adventure_shares (
    adventure_slug      TEXT NOT NULL,
    org_id              BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    granted_by_user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    granted_at          TEXT NOT NULL DEFAULT now_text(),
    PRIMARY KEY (adventure_slug, org_id)
);

CREATE INDEX idx_shares_org ON adventure_shares(org_id);
