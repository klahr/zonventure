-- The answer that took a team out of a puzzle, so a switch node can
-- route on it (internal/graph: SwitchConfig).
--
-- Per node, for a switch that names the puzzle it reads; and the latest
-- one per team, for a switch that reads whatever answer came down the
-- wire. Existing rows have no answer, which a switch treats as
-- "otherwise".
ALTER TABLE team_node_state ADD COLUMN answer TEXT NOT NULL DEFAULT '';
ALTER TABLE team_graph_state ADD COLUMN last_answer TEXT NOT NULL DEFAULT '';
