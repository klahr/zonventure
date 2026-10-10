-- Review for what listings show but don't own (listing review, 0123-0126):
-- the organisation's page, the adventure's presentation, and files for
-- an event's players.
--
-- The organisation's page (tagline, website, logo, cover) and an
-- adventure's presentation (title, description, author, intro, outro,
-- meeting point, area, note to buyers, icon, cover picture) are read
-- in dozens of places — pages, the player app, emails, calendar
-- invites. Rather than give each an approved copy, review holds back
-- the edit: once something of theirs is for sale, an unverified
-- organisation's edit to these is kept as a proposal, and only an
-- approval writes it to the organisation or the adventure. Everything
-- that reads them then shows approved content. Deleting a picture
-- still takes it down at once.
--
-- A proposal goes through the same review as a listing — submitted
-- with its creator's word, approved or rejected, logged in the hash
-- chain — under the kinds "org" and "adventure". Its columns are named
-- as a listing's so the same review code reads both.
CREATE TABLE presentation_proposals (
    id                    BIGSERIAL PRIMARY KEY,
    kind                  TEXT NOT NULL CHECK (kind IN ('org', 'adventure')),
    -- organizations.id for org, adventures.id for adventure.
    target_id             BIGINT NOT NULL,
    org_id                BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- Proposed values by column: tagline, public_website; title,
    -- description, author, intro_text, outro_text, start_label, area,
    -- buyer_note, look_cover (an adventure_media id).
    fields                JSONB NOT NULL DEFAULT '{}',
    -- An org's logo, or an adventure's icon.
    image_mime_type       TEXT,
    image_content         BYTEA,
    -- An org's cover.
    cover_mime_type       TEXT,
    cover_content         BYTEA,
    listing_review        TEXT NOT NULL DEFAULT 'draft'
                          CHECK (listing_review IN ('draft', 'pending', 'approved', 'rejected')),
    listing_submitted_at  TIMESTAMPTZ,
    listing_attested_at   TIMESTAMPTZ,
    listing_attested_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    listing_reviewed_at   TIMESTAMPTZ,
    listing_reviewed_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    listing_reject_reason TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One open proposal per organisation or adventure: edits gather in it.
CREATE UNIQUE INDEX idx_presentation_proposals_open ON presentation_proposals (kind, target_id)
    WHERE listing_review <> 'approved';
CREATE INDEX idx_presentation_proposals_pending ON presentation_proposals (listing_submitted_at)
    WHERE listing_review = 'pending';

-- The log names these kinds too.
ALTER TABLE listing_review_log DROP CONSTRAINT listing_review_log_kind_check;
ALTER TABLE listing_review_log ADD CONSTRAINT listing_review_log_kind_check
    CHECK (kind IN ('event', 'party', 'org', 'adventure'));

-- Files for an event's players: on an event sold in the store, a file
-- reaches players once the listing is approved with it. Files players
-- have today stay theirs.
ALTER TABLE event_files ADD COLUMN approved_at TIMESTAMPTZ;
UPDATE event_files SET approved_at = created_at;
