-- A role's kit item handed to another player (graph.Role.Kit): the
-- giver's phone shows a code, a teammate's phone scans it, and the item
-- is theirs to use from then on, whatever roles change hands later.
CREATE TABLE team_item_holders (
    team_id  BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    item     TEXT        NOT NULL,
    slot_id  BIGINT      NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    given_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, item)
);

-- The codes the giver's phone shows: one live code per item, good for
-- one scan by a teammate, for a few minutes.
CREATE TABLE item_gifts (
    code       TEXT        PRIMARY KEY,
    team_id    BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    item       TEXT        NOT NULL,
    from_slot  BIGINT      NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (team_id, item)
);
