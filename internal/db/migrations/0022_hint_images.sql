-- Per-hint images. Mirrors adventure_page_images in shape so the
-- authoring + serving code can reuse the same imageutil.Shrink path
-- and the same imageView shape on the wire.
--
-- ON DELETE CASCADE on the FK matters more than for puzzle images:
-- the hint editor's upsert flow can DELETE individual hint rows when
-- the author removes them from the editor list, and we need their
-- attached images to disappear with them rather than orphan.
--
-- alt_text defaults to '' so the column is non-NULL and parity with
-- adventure_page_images.alt_text (added in 0018) keeps the loader
-- helpers symmetric.
CREATE TABLE adventure_page_hint_images (
    id          BIGSERIAL PRIMARY KEY,
    hint_id     BIGINT  NOT NULL REFERENCES adventure_page_hints(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    mime_type   TEXT    NOT NULL,
    size_bytes  BIGINT  NOT NULL,
    content     BYTEA   NOT NULL,
    alt_text    TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_hint_images ON adventure_page_hint_images(hint_id, idx);
