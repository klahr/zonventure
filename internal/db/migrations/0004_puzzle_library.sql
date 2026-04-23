-- Puzzle library: allow adventure_id to be NULL so a puzzle can live in
-- the library without being attached to any adventure. Attached puzzles
-- keep the original ON DELETE CASCADE behaviour so deleting an adventure
-- still tears down its pages.
--
-- Also adds a `notes` column for internal author metadata (not shown to
-- players — this is an authoring scratchpad, e.g. "that statue by the
-- fountain, check opening hours").
--
-- SQLite can't make an existing NOT NULL column nullable in-place, so
-- the canonical recipe is table-rename → copy → swap. Foreign keys and
-- the unique(adventure_id, idx) constraint survive the swap because
-- SQLite treats each NULL adventure_id as distinct — library puzzles
-- can share any idx without conflict.
PRAGMA foreign_keys = OFF;

CREATE TABLE adventure_pages_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    adventure_id INTEGER REFERENCES adventures(id) ON DELETE CASCADE,
    idx          INTEGER NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    prompt       TEXT NOT NULL DEFAULT '',
    hint         TEXT NOT NULL DEFAULT '',
    accept       TEXT NOT NULL DEFAULT '[]',
    lat          REAL NOT NULL DEFAULT 0,
    lng          REAL NOT NULL DEFAULT 0,
    radius_m     REAL,
    disabled     INTEGER NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT '',
    UNIQUE(adventure_id, idx)
);

INSERT INTO adventure_pages_new
    (id, adventure_id, idx, title, prompt, hint, accept, lat, lng, radius_m, disabled, notes)
SELECT id, adventure_id, idx, title, prompt, hint, accept, lat, lng, radius_m, disabled, ''
FROM adventure_pages;

DROP TABLE adventure_pages;
ALTER TABLE adventure_pages_new RENAME TO adventure_pages;

CREATE INDEX idx_pages_adventure ON adventure_pages(adventure_id, idx);

PRAGMA foreign_keys = ON;
