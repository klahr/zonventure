-- Age-gate / minor data attestation. Runs that haven't been attested
-- can't start; the arranger ticks "no minors will participate, OR I
-- have parental consent for any under-13 players" at run creation.
-- Stored as a timestamp so an audit can see when consent was given.
-- Existing rows are backfilled to "now" so they aren't blocked from
-- starting after the migration runs.
ALTER TABLE adventure_runs ADD COLUMN attested_no_minors_at TEXT;
UPDATE adventure_runs SET attested_no_minors_at = now_text() WHERE attested_no_minors_at IS NULL;
