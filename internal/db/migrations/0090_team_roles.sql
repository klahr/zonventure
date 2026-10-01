-- The roles a team's players have claimed (graph.Role): one player per
-- role per team. A role nobody has claimed is dealt as the team is
-- read, never stored, so a player who joins later shares the rest.
CREATE TABLE team_roles (
    team_id    BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role       TEXT   NOT NULL,
    slot_id    BIGINT NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    claimed_at TEXT   NOT NULL DEFAULT now_text(),
    PRIMARY KEY (team_id, role)
);
CREATE INDEX idx_team_roles_slot ON team_roles(slot_id);
