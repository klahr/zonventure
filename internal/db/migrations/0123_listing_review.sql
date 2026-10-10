-- Listing review: what an organisation puts up for sale (a self-guided
-- event in its store, a party offer on its page) goes public only once
-- a platform admin has approved it. A verified creator's listings are
-- approved as they are saved.
--
-- The organisation edits the draft — the columns it has always edited
-- on events and party_offers. What the public sees, and what a buyer
-- pays, is the approved copy in event_listings / party_offer_listings:
-- a listing is public only when it has one. Approving copies the draft
-- over it; an edit to a live listing goes back to review while the
-- approved copy stays up.
--
-- listing_review is the draft's state: NULL when there is nothing to
-- review (not for sale, no price yet), else pending, approved or
-- rejected. listing_submitted_at is when the draft under review was
-- last changed: a reviewer's decision names it, so an edit made while
-- they looked can't be approved unseen.

-- Distinct from organizations.trusted, which allows invoice billing.
ALTER TABLE organizations
    ADD COLUMN verified_creator BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE events
    ADD COLUMN listing_review        TEXT CHECK (listing_review IN ('pending', 'approved', 'rejected')),
    ADD COLUMN listing_submitted_at  TIMESTAMPTZ,
    ADD COLUMN listing_reviewed_at   TIMESTAMPTZ,
    ADD COLUMN listing_reviewed_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN listing_reject_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE party_offers
    ADD COLUMN listing_review        TEXT CHECK (listing_review IN ('pending', 'approved', 'rejected')),
    ADD COLUMN listing_submitted_at  TIMESTAMPTZ,
    ADD COLUMN listing_reviewed_at   TIMESTAMPTZ,
    ADD COLUMN listing_reviewed_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN listing_reject_reason TEXT NOT NULL DEFAULT '';

-- The review queue reads pending rows only.
CREATE INDEX idx_events_listing_pending ON events (listing_submitted_at) WHERE listing_review = 'pending';
CREATE INDEX idx_party_offers_listing_pending ON party_offers (listing_submitted_at) WHERE listing_review = 'pending';

-- The approved copy of a self-guided event's store listing. The cover
-- is copied too, so a new one waits for review behind the old.
CREATE TABLE event_listings (
    event_id        BIGINT PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    blurb           TEXT NOT NULL,
    price_cents     INTEGER NOT NULL,
    currency        TEXT NOT NULL,
    team_size       INTEGER NOT NULL,
    cover_mime_type TEXT,
    cover_content   BYTEA,
    approved_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_by     BIGINT REFERENCES users(id) ON DELETE SET NULL
);

-- The approved copy of a party offer.
CREATE TABLE party_offer_listings (
    offer_id      BIGINT PRIMARY KEY REFERENCES party_offers(id) ON DELETE CASCADE,
    blurb         TEXT NOT NULL,
    base_cents    INTEGER NOT NULL,
    per_kid_cents INTEGER NOT NULL,
    currency      TEXT NOT NULL,
    max_kids      INTEGER NOT NULL,
    approved_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_by   BIGINT REFERENCES users(id) ON DELETE SET NULL
);

-- What is for sale today stays for sale: approved as it is.
INSERT INTO event_listings (event_id, name, blurb, price_cents, currency, team_size, cover_mime_type, cover_content)
SELECT id, name, public_blurb, ticket_price_cents, ticket_currency, ticket_team_size, cover_image_mime_type, cover_image_content
FROM events
WHERE public_for_sale AND ticket_price_cents IS NOT NULL;

UPDATE events SET listing_review = 'approved', listing_reviewed_at = now()
WHERE public_for_sale AND ticket_price_cents IS NOT NULL;

INSERT INTO party_offer_listings (offer_id, blurb, base_cents, per_kid_cents, currency, max_kids)
SELECT id, blurb, base_cents, per_kid_cents, currency, max_kids
FROM party_offers
WHERE enabled;

UPDATE party_offers SET listing_review = 'approved', listing_reviewed_at = now()
WHERE enabled;
