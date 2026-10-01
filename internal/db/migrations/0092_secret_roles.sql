-- Secret roles (graph.Role.Secret): who on a team holds each, dealt at
-- random once, when the team makes its first move. A row with no slot
-- is a role dealt to no one — a team too small, or secret roles off —
-- so it is not dealt again later, mid-game.
CREATE TABLE team_secrets (
    team_id  BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role     TEXT        NOT NULL,
    slot_id  BIGINT      REFERENCES player_slots(id) ON DELETE SET NULL,
    dealt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, role)
);
CREATE INDEX idx_team_secrets_slot ON team_secrets(slot_id) WHERE slot_id IS NOT NULL;

-- An arranger may run an adventure with a saboteur without one: for a
-- school group, a team-building day.
ALTER TABLE events ADD COLUMN secret_roles BOOLEAN NOT NULL DEFAULT TRUE;
