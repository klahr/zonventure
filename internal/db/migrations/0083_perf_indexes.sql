-- Indexes the hot paths and the cascades were missing.
--
-- Foreign keys into player_slots, teams and users had none on these
-- tables, so deleting a slot, team or event — the simulator does on
-- every run — and a player forgetting their data each scanned the whole
-- table once per slot.
CREATE INDEX IF NOT EXISTS idx_pings_slot ON location_pings(slot_id);
CREATE INDEX IF NOT EXISTS idx_attempts_slot ON answer_attempts(slot_id);
CREATE INDEX IF NOT EXISTS idx_event_activity_actor_slot ON event_activity(actor_slot_id) WHERE actor_slot_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_event_activity_actor_user ON event_activity(actor_user_id) WHERE actor_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_team_photos_slot ON team_photos(slot_id) WHERE slot_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_team_block_votes_slot ON team_block_votes(slot_id);
CREATE INDEX IF NOT EXISTS idx_graph_effect_outbox_team ON graph_effect_outbox(team_id);

-- A team's activity by kind: when each team last answered right, for
-- the ranking and the live board, read per team on every push. It also
-- serves the team_id foreign key.
CREATE INDEX IF NOT EXISTS idx_event_activity_team_kind ON event_activity(team_id, kind, created_at DESC) WHERE team_id IS NOT NULL;
