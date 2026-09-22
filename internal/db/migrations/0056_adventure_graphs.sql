-- The graph adventure model.
--
-- An adventure's structure becomes a directed graph of seven node types
-- (see internal/graph). This migration adds storage for authored graphs
-- and for the per-team runtime state the graph engine (internal/play)
-- keeps while an event runs.
--
-- It is purely additive. The lane/page tables and the existing player
-- runtime are untouched, and an event runs on the graph engine only
-- when it has a graph pinned (events.graph_id). That lets the two
-- models coexist through the migration instead of forcing a cutover.
--
-- New tables use TIMESTAMPTZ rather than the TEXT-plus-now_text()
-- convention of the older tables. The reason for TEXT was that the
-- hand-written front-end parsed that exact format; the graph engine has
-- no such front-end, and time limits, lockouts and hint delays are all
-- range comparisons that belong in the database's own time type.

-- Authored graphs, one row per saved version.
--
-- The graph is stored as a single JSONB document rather than as node
-- and edge rows. It is always read, validated and written as a whole —
-- the editor saves the entire canvas, the validator needs the entire
-- graph to check reachability, and the runtime loads it into memory —
-- so rows would add joins and partial-write states without enabling a
-- single query anything needs. Nodes are addressed by slug inside the
-- document, which is what makes a graph survive export and re-import.
--
-- Versions are append-only. Saving creates a new version; an event pins
-- the version it started with, so an author editing mid-event can never
-- move the ground under a team that is already playing.
CREATE TABLE adventure_graphs (
    id                 BIGSERIAL PRIMARY KEY,
    adventure_id       BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    version            INTEGER NOT NULL,
    graph              JSONB NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (adventure_id, version)
);

CREATE INDEX idx_adventure_graphs_latest ON adventure_graphs (adventure_id, version DESC);

-- The graph version an event runs. Set once, when the event starts;
-- NULL means the event runs on the lane engine.
ALTER TABLE events ADD COLUMN graph_id BIGINT REFERENCES adventure_graphs(id);

-- One row per team on the graph engine: the cursor, and the running
-- totals that would otherwise need re-deriving on every request.
--
-- current_node is a slug. NULL before the team is placed and after it
-- finishes. The runtime updates it with a compare-and-swap on the old
-- value, so two teammates submitting at once cannot both advance.
CREATE TABLE team_graph_state (
    team_id         BIGINT PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
    event_id        BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    graph_id        BIGINT NOT NULL REFERENCES adventure_graphs(id),
    current_node    TEXT,
    node_entered_at TIMESTAMPTZ,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    outcome         TEXT CHECK (outcome IN ('complete', 'fail')),
    -- Scores are fractional because a near-miss numeric answer earns a
    -- fraction of its points.
    score           DOUBLE PRECISION NOT NULL DEFAULT 0,
    penalty_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    penalty_seconds INTEGER NOT NULL DEFAULT 0,
    theme           TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_team_graph_state_event ON team_graph_state (event_id);

-- Per-team, per-node history. This is the table that makes keys
-- unnecessary: "has the team solved the bakery?" and "which door did
-- they choose?" are rows here, written as a side effect of playing,
-- rather than tokens an author has to remember to grant.
CREATE TABLE team_node_state (
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    node            TEXT NOT NULL,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    solved_at       TIMESTAMPTZ,
    -- The output port the team left by: which answer they gave.
    outcome         TEXT,
    attempts        INTEGER NOT NULL DEFAULT 0,
    blocked_until   TIMESTAMPTZ,
    points          DOUBLE PRECISION NOT NULL DEFAULT 0,
    hints_revealed  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (team_id, node)
);

-- Hub branches a team has been sent into.
--
-- returned_at is set when the team comes back through the hub (or
-- finishes). A branch with entered_at and no returned_at is one the
-- team is currently inside, which is exactly what capacity occupancy
-- counts — hence event_id here and the partial index below.
CREATE TABLE team_hub_branches (
    team_id     BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    hub         TEXT NOT NULL,
    port        TEXT NOT NULL,
    entered_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    returned_at TIMESTAMPTZ,
    PRIMARY KEY (team_id, hub, port)
);

CREATE INDEX idx_team_hub_branches_occupancy
    ON team_hub_branches (event_id, hub, port)
    WHERE returned_at IS NULL;

-- Inventory. Flavour only — the player sees it, but no gate requires it.
CREATE TABLE team_items (
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    item    TEXT NOT NULL,
    qty     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (team_id, item)
);

CREATE TABLE team_journal (
    team_id     BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    entry       TEXT NOT NULL,
    unlocked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, entry)
);

-- Event-wide shared counters: the one piece of state that is genuinely
-- not per-team, and so the one thing history cannot answer.
CREATE TABLE event_counters (
    event_id BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    counter  TEXT NOT NULL,
    value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (event_id, counter)
);

-- Effects with an outside-world consequence — email and webhook — are
-- queued here inside the same transaction that moved the team, and
-- delivered by a worker afterwards. Delivering inline would either hold
-- a database transaction open across a network call or risk sending an
-- email for a move that then rolled back.
CREATE TABLE graph_effect_outbox (
    id           BIGSERIAL PRIMARY KEY,
    event_id     BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    team_id      BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    node         TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('email', 'webhook')),
    payload      JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX idx_graph_effect_outbox_pending
    ON graph_effect_outbox (created_at)
    WHERE delivered_at IS NULL;
