-- Where an author last stood, reported by their phone while they build an
-- adventure on site, so an assistant can place a zone "here". One row per
-- user, overwritten on each fix: this is not a track.
CREATE TABLE creator_positions (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    lat        DOUBLE PRECISION NOT NULL,
    lng        DOUBLE PRECISION NOT NULL,
    accuracy_m DOUBLE PRECISION NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
