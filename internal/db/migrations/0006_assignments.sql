-- Replace the "grant" model (unlimited uses) with single-use assignments.
-- Each assignment permits exactly one run and fixes the maximum number of
-- teams that run may host. Buying more assignments is a separate commercial
-- flow outside this repo.

DROP TABLE IF EXISTS arranger_adventures;

CREATE TABLE adventure_assignments (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    arranger_user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    adventure_slug    TEXT NOT NULL,
    max_teams         INTEGER NOT NULL CHECK (max_teams > 0),
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_assignments_arranger ON adventure_assignments(arranger_user_id);

-- An assignment is consumed by exactly one run (or none yet). The unique
-- index over non-NULL assignment_id keeps the one-shot property enforced
-- at the database level.
ALTER TABLE adventure_runs ADD COLUMN assignment_id INTEGER REFERENCES adventure_assignments(id);
CREATE UNIQUE INDEX idx_runs_assignment ON adventure_runs(assignment_id) WHERE assignment_id IS NOT NULL;
