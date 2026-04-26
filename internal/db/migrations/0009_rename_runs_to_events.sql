-- Rebrand: "Run" / "Omgång" → "Event". The DB layer follows so app
-- code, SQL, and customer-facing webhook payloads can speak one
-- vocabulary. Postgres FKs reference tables by OID, so renaming
-- the parent doesn't break dependents — only the column-level
-- references need explicit ALTERs.

-- adventure_runs is the top-level "scheduled run of an adventure"
-- entity. Renamed to events.
ALTER TABLE adventure_runs RENAME TO events;

-- run_events was the activity feed (answer attempts, team
-- completions, gate reaches, lifecycle transitions). Now an
-- "event_activity" feed of activity within an event — avoids the
-- doubled-up "events_events" naming.
ALTER TABLE run_events RENAME TO event_activity;

-- Foreign-key columns that pointed at adventure_runs.id by holding
-- a "run_id" name. Each one keeps its FK relationship through the
-- table rename above; only the column label changes.
ALTER TABLE teams           RENAME COLUMN run_id TO event_id;
ALTER TABLE location_pings  RENAME COLUMN run_id TO event_id;
ALTER TABLE event_activity  RENAME COLUMN run_id TO event_id;

-- Indexes carry their old names. Renamed to match the new vocab so
-- a Postgres operator reading EXPLAIN output isn't surprised.
ALTER INDEX idx_runs_state         RENAME TO idx_events_state;
ALTER INDEX idx_runs_org           RENAME TO idx_events_org;
ALTER INDEX idx_runs_assignment    RENAME TO idx_events_assignment;
ALTER INDEX idx_teams_run          RENAME TO idx_teams_event;
ALTER INDEX idx_pings_run_ts       RENAME TO idx_pings_event_ts;
ALTER INDEX idx_run_events_run_ts  RENAME TO idx_event_activity_event_ts;

-- The teams table had an anonymous UNIQUE(run_id, name) constraint.
-- Postgres auto-named it teams_run_id_name_key when the table was
-- created; the column rename above silently updated its column
-- reference but left the *constraint name* with "run_id" in it.
-- Rename so a future operator reading \d teams sees consistent vocab.
ALTER TABLE teams RENAME CONSTRAINT teams_run_id_name_key TO teams_event_id_name_key;
