-- A short-lived link an assistant uploads pictures and sounds through
-- (curl -F file=@…), so the bytes go from the author's disk to the
-- server without passing through the model as base64. It uploads to one
-- adventure, as the user who asked for it, a limited number of times.
CREATE TABLE media_upload_tickets (
    token_hash   BYTEA PRIMARY KEY,
    adventure_id BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id       BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    uses         INTEGER NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ NOT NULL
);
