-- Changes to an organisation's public page (name, tagline, website,
-- logo, cover) made while something of its is for sale apply at once
-- and are reviewed afterwards: one row per field changed since the last
-- review, keeping the value before, so a reviewer can approve it or put
-- the previous one back. Several changes to a field before a review
-- keep the first previous value: the one a reviewer last saw.
--
-- Organisations a platform admin trusts (verified creators, invoice
-- billing, an own domain) have none: their changes need no review.
CREATE TABLE org_page_changes (
    id               BIGSERIAL PRIMARY KEY,
    org_id           BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    field            TEXT NOT NULL CHECK (field IN ('name', 'tagline', 'public_website', 'logo', 'cover')),
    -- The value before: text for the text fields; mime and content for
    -- a picture, none when there was no picture.
    previous_text    TEXT,
    previous_mime    TEXT,
    previous_content BYTEA,
    changed_by       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    changed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at      TIMESTAMPTZ,
    reviewed_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    -- approved, or rolled_back with the reason the organisation is told.
    outcome          TEXT CHECK (outcome IN ('approved', 'rolled_back')),
    reason           TEXT NOT NULL DEFAULT ''
);
-- At most one change per field waits for review.
CREATE UNIQUE INDEX idx_org_page_changes_open ON org_page_changes(org_id, field) WHERE reviewed_at IS NULL;
