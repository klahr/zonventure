-- A team split up into groups at a split step (graph.SplitConfig) is in
-- several places at once until they meet again at its join: split is the
-- split, and strands each group's place, path and players, as
-- session.Strand encodes them. current_node stays the split meanwhile, so
-- everything that reads where a team is sees it there.
ALTER TABLE team_graph_state ADD COLUMN split TEXT;
ALTER TABLE team_graph_state ADD COLUMN strands JSONB;
