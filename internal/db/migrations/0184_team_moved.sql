-- Who moved the team onto its step (session Cursor), for its phones to
-- say so: moved, the organisers' rescue; moved_by, the phone (slot) whose
-- action did, 0 for none. Both as the last move left them. A split
-- team's groups carry theirs in strands.
ALTER TABLE team_graph_state ADD COLUMN moved BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE team_graph_state ADD COLUMN moved_by BIGINT NOT NULL DEFAULT 0;

-- A team the organisers moved before this, still where they put it —
-- moved there no earlier than it came there — as the activity log has
-- it: its phones say so as they did.
UPDATE team_graph_state gs SET moved = true
WHERE gs.split IS NULL AND gs.completed_at IS NULL AND gs.current_node IS NOT NULL AND gs.current_node = (
  SELECT a.payload::jsonb->>'to' FROM event_activity a
  WHERE a.team_id = gs.team_id AND a.kind = 'team.unlocked'
    -- The app's clock stamped the arrival, the database's the log: a few
    -- seconds' leeway for the two.
    AND a.created_at >= to_char((gs.node_entered_at - interval '5 seconds') AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
  ORDER BY a.created_at DESC, a.id DESC LIMIT 1);
