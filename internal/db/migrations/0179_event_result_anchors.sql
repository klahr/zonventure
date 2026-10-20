-- An event's results fingerprint, mailed to its organisation's admins
-- once the event is over (admin.AnchorEventResults): a copy outside the
-- database, so even a results record rewritten whole, hashes and all,
-- shows — its fingerprint no longer matches the mail. One row per event
-- mailed; it goes with its event.

CREATE TABLE event_result_anchors (
    event_id    BIGINT PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    entries     BIGINT NOT NULL,
    problems    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
