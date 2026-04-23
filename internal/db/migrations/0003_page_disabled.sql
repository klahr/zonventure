-- Disabled pages are preserved in the adventure but skipped by players
-- during a run. Authors disable instead of delete so a flaky puzzle can
-- be taken out of rotation mid-run without losing its content or gate
-- coordinates — flip the bit back to re-enable it for future runs.
ALTER TABLE adventure_pages ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
