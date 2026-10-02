-- A pause freezes a team's clocks. start_paused and node_paused are the
-- event's pause clock (paused_seconds plus any open pause) when the team
-- started and when it entered its current node; the engine leaves out of
-- every elapsed time and deadline the pauses since.
--
-- Teams already playing take the clock as it reads now: pauses before
-- this release keep counting, as they did, and only later ones are left
-- out.

ALTER TABLE team_graph_state
    ADD COLUMN start_paused INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN node_paused  INTEGER NOT NULL DEFAULT 0;

UPDATE team_graph_state s
   SET start_paused = e.paused_seconds + COALESCE(
           GREATEST(0, EXTRACT(EPOCH FROM (now() AT TIME ZONE 'UTC') - e.paused_at::timestamp))::int, 0),
       node_paused = e.paused_seconds + COALESCE(
           GREATEST(0, EXTRACT(EPOCH FROM (now() AT TIME ZONE 'UTC') - e.paused_at::timestamp))::int, 0)
  FROM events e
 WHERE e.id = s.event_id;
