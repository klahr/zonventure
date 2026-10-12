-- The adventure version a self-guided event moves to once its listing is
-- approved: a newer one its organiser asked for. Until then teams play
-- the version the event has (graph_id), the one last reviewed. NULL is
-- no move asked for.
ALTER TABLE events ADD COLUMN next_graph_id BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL;
