-- Pictures a team takes for a photo block. An organiser approves or
-- denies each later; the team moves on either way. worth is what the
-- block paid when the picture came in, and awarded what it has added to
-- the team's score so far, so a changed verdict adds or takes back only
-- the difference. A denied picture stays, as history, and the team may
-- take another; an approved one ends the challenge.
CREATE TABLE team_photos (
    id          BIGSERIAL PRIMARY KEY,
    team_id     BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slot_id     BIGINT REFERENCES player_slots(id) ON DELETE SET NULL,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    node        TEXT NOT NULL,
    block_id    TEXT NOT NULL,
    content     BYTEA NOT NULL,
    mime        TEXT NOT NULL,
    worth       INT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    awarded     INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    reviewed_by BIGINT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_team_photos_event ON team_photos (event_id, status);
CREATE INDEX idx_team_photos_block ON team_photos (team_id, node, block_id);

-- Each player's vote on a vote block, one per player. Votes never route
-- or score; the team's phones show the tally.
CREATE TABLE team_block_votes (
    team_id  BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    node     TEXT NOT NULL,
    block_id TEXT NOT NULL,
    slot_id  BIGINT NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    choice   TEXT NOT NULL,
    voted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, node, block_id, slot_id)
);
