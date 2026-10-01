-- What each player has done on a step that needs them all: turned their
-- key in a two-key launch (value: the posts they stood at), or given a
-- right answer to a unanimous puzzle (value: the route it leads by).
-- One row per player per step; cleared when the team leaves the step.
CREATE TABLE team_presses (
    team_id    BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    node       TEXT        NOT NULL,
    slot_id    BIGINT      NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    value      TEXT        NOT NULL DEFAULT '',
    pressed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (team_id, node, slot_id)
);
CREATE INDEX idx_team_presses_slot ON team_presses(slot_id);
