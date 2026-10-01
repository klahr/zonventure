-- A person's own picture, shown wherever they appear: the account menu,
-- who is here, their comments, the steps they own. Inline on the row,
-- as team and adventure avatars are (see migrations 0001, 0030);
-- avatar_updated_at versions its address, so a new picture shows at once.
ALTER TABLE users
    ADD COLUMN avatar_mime_type  TEXT,
    ADD COLUMN avatar_size_bytes BIGINT,
    ADD COLUMN avatar_content    BYTEA,
    ADD COLUMN avatar_updated_at TIMESTAMPTZ;
