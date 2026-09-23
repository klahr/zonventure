-- The language a team plays a translated adventure in; empty is the
-- adventure's main language.
ALTER TABLE team_graph_state ADD COLUMN lang TEXT NOT NULL DEFAULT '';
