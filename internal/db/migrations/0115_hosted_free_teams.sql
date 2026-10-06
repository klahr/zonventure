-- A hosted event of no blocks is free: it holds FreeTeams teams per game
-- (internal/admin/hosted.go). Paying for blocks replaces that allowance.
ALTER TABLE events DROP CONSTRAINT IF EXISTS events_hosted_blocks_check;
ALTER TABLE events ADD CONSTRAINT events_hosted_blocks_check CHECK (hosted_blocks >= 0);
