-- Audio attachments for puzzles and hints. Mirrors the image tables
-- (0001 + 0022) so the authoring/serving code can reuse the same
-- patterns: same ordered idx, same per-row blob, same cascade delete
-- on the parent so removing a puzzle / hint cleans up its media.
--
-- Where this differs from images:
--   * No re-encoding step on upload — the binary lands in `content`
--     verbatim. Browsers handle the format negotiation via the
--     <audio> element's MIME type.
--   * `description` instead of `alt_text` because the WCAG fallback
--     for audio is a transcript or summary, not a screen-reader-only
--     short-phrase. Same NOT NULL DEFAULT '' shape so loaders stay
--     symmetric.
CREATE TABLE adventure_page_audio (
    id          BIGSERIAL PRIMARY KEY,
    page_id     BIGINT  NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    mime_type   TEXT    NOT NULL,
    size_bytes  BIGINT  NOT NULL,
    content     BYTEA   NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_page_audio ON adventure_page_audio(page_id, idx);

CREATE TABLE adventure_page_hint_audio (
    id          BIGSERIAL PRIMARY KEY,
    hint_id     BIGINT  NOT NULL REFERENCES adventure_page_hints(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    mime_type   TEXT    NOT NULL,
    size_bytes  BIGINT  NOT NULL,
    content     BYTEA   NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_hint_audio ON adventure_page_hint_audio(hint_id, idx);
