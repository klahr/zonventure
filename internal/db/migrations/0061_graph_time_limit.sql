-- An adventure-wide time limit moves a team to a chosen step once, with a
-- message; this records that it has, so it never fires twice.
ALTER TABLE team_graph_state ADD COLUMN timed_out BOOLEAN NOT NULL DEFAULT false;
