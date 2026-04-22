-- run_events is the activity feed for a run: every administrative action,
-- lifecycle transition, answer attempt, and gate reach lands here so the
-- arranger can see a single chronological story of what happened.
CREATE TABLE run_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL REFERENCES adventure_runs(id) ON DELETE CASCADE,
    team_id    INTEGER REFERENCES teams(id) ON DELETE SET NULL,
    actor_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_run_events_run_ts ON run_events(run_id, created_at);
