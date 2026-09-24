-- A team's answers to quiz-walk questions and the tie-breaker, one per
-- block. They never route and only score when the team finishes, so they
-- live beside team_node_state rather than in it.
CREATE TABLE team_block_picks (
    team_id   BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    node      TEXT NOT NULL,
    block_id  TEXT NOT NULL,
    choice    TEXT NOT NULL,
    picked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, node, block_id)
);
