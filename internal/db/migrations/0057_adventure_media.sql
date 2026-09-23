-- Images and audio for graph adventures.
--
-- Lane pages keep their own per-page tables (adventure_page_images and
-- friends). A graph node has no row to hang media off — nodes live inside
-- the graph document — so media belongs to the adventure, and a node
-- refers to it by id. That also lets one picture be used by two steps,
-- and survives a node being renamed or a new graph version being saved.
--
-- sha256 deduplicates per adventure: converting the same lanes twice, or
-- uploading the same photo twice, keeps one copy.
--
-- Who may fetch a row is decided at request time from the graph, not
-- stored here: a player sees media on steps their team has reached, and
-- in hints it has revealed (internal/graph/session: CanSeeMedia).
CREATE TABLE adventure_media (
    id                 BIGSERIAL PRIMARY KEY,
    adventure_id       BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL CHECK (kind IN ('image', 'audio')),
    mime_type          TEXT NOT NULL,
    size_bytes         BIGINT NOT NULL,
    sha256             TEXT NOT NULL,
    content            BYTEA NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (adventure_id, sha256)
);
