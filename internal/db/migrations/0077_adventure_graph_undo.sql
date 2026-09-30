-- What an adventure's draft was before each edit an assistant made with
-- edit_adventure_graph, so "undo that" can put it back. Only the latest
-- few are kept; this is an undo stack, not history — releases are that.
CREATE TABLE adventure_graph_undo (
    id           BIGSERIAL PRIMARY KEY,
    adventure_id BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    graph        JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX adventure_graph_undo_adventure ON adventure_graph_undo (adventure_id, id);
