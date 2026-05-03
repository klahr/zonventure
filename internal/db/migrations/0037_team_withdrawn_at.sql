-- Track teams that gave up mid-event. Mirrors completed_at: nil = still
-- active, non-nil = withdrew at that timestamp. A withdrawn team
-- stays in the recap (for honest reporting) but is grouped separately
-- below the leaderboard's unfinished section, and the "stuck"
-- detector ignores it so the race director isn't pestered to chase
-- a team that already dropped out.

ALTER TABLE teams ADD COLUMN withdrawn_at TEXT;
