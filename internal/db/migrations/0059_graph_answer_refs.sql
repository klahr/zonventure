-- Which accepted answer took a team out of a puzzle, by its stable id
-- (internal/graph/match: Matcher.ID), so a crossroads path can refer to
-- an answer instead of repeating its text. Empty for answers without an
-- id, and for rows written before this.
ALTER TABLE team_node_state ADD COLUMN match_id TEXT NOT NULL DEFAULT '';
