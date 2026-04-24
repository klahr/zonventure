-- Multi-hint support per puzzle, with per-hint delay gating.
--
-- Authors add 0..N hints; each has a delay_seconds that counts from
-- the moment a team reached the current puzzle (stamped in teams.
-- current_page_started_at below). The player's UI shows a single
-- "Reveal hint" button that unlocks hints in order; the delay keeps
-- early help from being handed out for free.
--
-- Existing adventure_pages.hint rows are backfilled as a single
-- delay=0 entry so no adventure loses content; the column stays in
-- place for now as legacy data but nothing reads it anymore.

CREATE TABLE adventure_page_hints (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    page_id        INTEGER NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    idx            INTEGER NOT NULL,
    text           TEXT    NOT NULL DEFAULT '',
    delay_seconds  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(page_id, idx)
);

CREATE INDEX idx_page_hints ON adventure_page_hints(page_id, idx);

-- Carry every existing non-empty hint over as the page's first (and
-- only) unlock-immediately hint.
INSERT INTO adventure_page_hints (page_id, idx, text, delay_seconds)
SELECT id, 0, hint, 0 FROM adventure_pages WHERE TRIM(hint) <> '';

-- Per-team timestamp for "when did this team become eligible for the
-- current puzzle". Used at render time to compute each hint's
-- unlocks_in_seconds. NULL means "not advanced since team creation";
-- the server falls back to adventure_runs.started_at in that case so
-- page-0 hints still unlock correctly for the run's original teams.
ALTER TABLE teams ADD COLUMN current_page_started_at TEXT;
