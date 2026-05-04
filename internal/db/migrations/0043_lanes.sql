-- Lanes: groups of puzzles within an adventure that share an ordering
-- mode. mode='linear' is the legacy behaviour (every team visits the
-- lane's puzzles in the same idx order). mode='shuffled' gives each
-- team a deterministic cyclic-shifted permutation of the lane's
-- puzzles, so teams spread out at the start instead of piling up on
-- puzzle 1.
--
-- Lanes themselves are sequenced (lane idx 0 first, then 1, ...).
-- When a team finishes every puzzle in their personal ordering of a
-- lane, the next lane unlocks. When every lane is exhausted, the
-- team is done — the team with the earliest completed_at wins. There
-- is no separate "finale" concept: the last puzzle a team solves is
-- whatever sits at the end of their last lane's permutation. If an
-- arranger wants every team to converge on a shared finish-line
-- puzzle, they make the last lane linear with a single puzzle.
--
-- Backfill preserves bit-for-bit legacy behaviour: every existing
-- adventure gets one default linear lane that owns all its current
-- pages, so the runtime keeps walking pages in idx order and stamps
-- completed_at when there's no enabled page after the current one
-- — exactly what the pre-lanes code did.

CREATE TABLE adventure_lanes (
    id           BIGSERIAL PRIMARY KEY,
    adventure_id BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    idx          INTEGER NOT NULL,
    mode         TEXT NOT NULL DEFAULT 'linear' CHECK (mode IN ('linear','shuffled')),
    -- Optional human label shown in the arranger's lane header. Empty
    -- string means "fall back to 'Lane N' in the UI" — we keep the
    -- name optional so single-lane adventures (the default after the
    -- backfill) don't grow chrome they don't need.
    name         TEXT NOT NULL DEFAULT '',
    UNIQUE (adventure_id, idx)
);

CREATE INDEX idx_lanes_adventure ON adventure_lanes(adventure_id, idx);

-- Pages get a lane pointer. NULL is tolerated for two reasons:
--   1. Library puzzles (adventure_id IS NULL) never belong to a lane.
--   2. Pages briefly between lane assignments during admin edits can
--      land here; the runtime falls back to "this adventure's default
--      lane" rather than crashing.
ALTER TABLE adventure_pages
    ADD COLUMN lane_id BIGINT REFERENCES adventure_lanes(id) ON DELETE SET NULL;

CREATE INDEX idx_pages_lane ON adventure_pages(lane_id);

-- Per-team permutation per lane. page_ids is a JSONB array of the
-- adventure_pages.id values in the order this team visits them.
-- Computed when the run transitions to running and frozen for the
-- run; insulates teams from any mid-run lane edits an arranger might
-- make. Missing rows are tolerated by the runtime — a fallback
-- cyclic-shift derivation runs on the fly so the column is never
-- load-bearing on its own.
CREATE TABLE team_lane_orderings (
    team_id  BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    lane_id  BIGINT NOT NULL REFERENCES adventure_lanes(id) ON DELETE CASCADE,
    page_ids JSONB NOT NULL,
    PRIMARY KEY (team_id, lane_id)
);

-- Team progress pointers. current_page_id is the source of truth
-- for "which page is this team on right now". current_lane_id caches
-- the lane membership so progression queries don't need a join.
-- Both NULL = team has no active page (pre-start, completed, or
-- post-migration unbackfilled). The runtime tolerates NULL by
-- looking up the first enabled page in the team's adventure as the
-- starting point — same fallback the legacy current_page_index=0
-- code path used.
ALTER TABLE teams
    ADD COLUMN current_page_id BIGINT REFERENCES adventure_pages(id) ON DELETE SET NULL,
    ADD COLUMN current_lane_id BIGINT REFERENCES adventure_lanes(id) ON DELETE SET NULL;

-- Backfill: one default linear lane per adventure.
INSERT INTO adventure_lanes (adventure_id, idx, mode)
SELECT id, 0, 'linear' FROM adventures;

-- Attach existing pages to their adventure's default lane. Library
-- puzzles (adventure_id IS NULL) stay lane-less.
UPDATE adventure_pages p
SET lane_id = (
    SELECT id FROM adventure_lanes l
    WHERE l.adventure_id = p.adventure_id AND l.idx = 0
)
WHERE p.adventure_id IS NOT NULL;

-- Backfill team pointers for in-flight teams. Completed teams keep
-- NULL pointers — completed_at is the source of truth there and we
-- don't need a cursor pointing into a finished run.
UPDATE teams t
SET current_lane_id = (
        SELECT l.id
        FROM adventure_lanes l
        JOIN events ev ON ev.id = t.event_id
        JOIN adventures a ON a.slug = ev.adventure_slug
        WHERE l.adventure_id = a.id AND l.idx = 0
    ),
    current_page_id = (
        SELECT p.id
        FROM adventure_pages p
        JOIN events ev ON ev.id = t.event_id
        JOIN adventures a ON a.slug = ev.adventure_slug
        WHERE p.adventure_id = a.id
          AND p.idx >= t.current_page_index
          AND p.disabled = 0
        ORDER BY p.idx
        LIMIT 1
    )
WHERE t.completed_at IS NULL;
