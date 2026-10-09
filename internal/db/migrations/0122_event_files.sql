-- Files an event's organisers hand its players ahead of the start: a
-- PDF with the rules, a map to print, a packing list. Players download
-- them from the lobby and the menu; they are never part of the
-- adventure, so a duplicated or re-run adventure doesn't carry them.
CREATE TABLE event_files (
    id         BIGSERIAL PRIMARY KEY,
    event_id   BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    content    BYTEA NOT NULL,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_event_files_event ON event_files (event_id, created_at);
