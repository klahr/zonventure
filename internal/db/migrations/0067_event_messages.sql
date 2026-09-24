-- Messages from an event's organisers to its teams: information beside
-- the game, not part of it. team_ids NULL is every team of the event;
-- otherwise only those listed.
CREATE TABLE event_messages (
    id         BIGSERIAL PRIMARY KEY,
    event_id   BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    team_ids   BIGINT[],
    text       TEXT NOT NULL,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_event_messages_event ON event_messages (event_id, created_at DESC);
