-- Content review (on top of listing review, 0123). The point of review
-- is to keep offensive content off the platform, so it covers everything
-- a listing puts in front of the public and its players: the listing,
-- the adventure version it plays, and the organisation's page.
--
-- An edit no longer goes to review by itself: it leaves the listing a
-- 'draft' until its creator submits it, confirming the content follows
-- the terms (listing_attested_at/_by). A verified creator's submission
-- is approved at once.
--
-- Approval pins the adventure version reviewed (graph_id on the approved
-- copy). A self-guided event plays the version it was created with, so
-- that is the one reviewed. A party offer's draft is its adventure's
-- latest published version; a booked party plays the approved one
-- (events.base_graph_id) until a newer one is submitted and approved.

ALTER TABLE events DROP CONSTRAINT events_listing_review_check;
ALTER TABLE events ADD CONSTRAINT events_listing_review_check
    CHECK (listing_review IN ('draft', 'pending', 'approved', 'rejected'));
ALTER TABLE party_offers DROP CONSTRAINT party_offers_listing_review_check;
ALTER TABLE party_offers ADD CONSTRAINT party_offers_listing_review_check
    CHECK (listing_review IN ('draft', 'pending', 'approved', 'rejected'));

ALTER TABLE events
    ADD COLUMN listing_attested_at TIMESTAMPTZ,
    ADD COLUMN listing_attested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    -- The adventure version a booked party plays: its offer's approved one.
    ADD COLUMN base_graph_id       BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL;

ALTER TABLE party_offers
    ADD COLUMN listing_attested_at TIMESTAMPTZ,
    ADD COLUMN listing_attested_by BIGINT REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE event_listings
    ADD COLUMN graph_id BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL;
ALTER TABLE party_offer_listings
    ADD COLUMN graph_id BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL;

-- What is approved today was approved with the version it plays now.
UPDATE event_listings l SET graph_id = e.graph_id FROM events e WHERE e.id = l.event_id;
UPDATE party_offer_listings l SET graph_id = a.published_graph_id
FROM party_offers o JOIN adventures a ON a.slug = o.adventure_slug
WHERE o.id = l.offer_id;

-- Who may review listings: chosen person by person by a platform admin
-- (platform admins too), so an AI assistant can have an account of its
-- own that reviews and does nothing else.
ALTER TABLE users ADD COLUMN listing_reviewer BOOLEAN NOT NULL DEFAULT FALSE;

-- Every submission and decision, for spot-checks of what an AI reviewer
-- (via = 'mcp') or a verified creator's automatic approval let through.
--
-- The log is tamper-evident: each entry's entry_hash is a SHA-256 over
-- the previous entry's hash and its own fields (listing_review_chain.go),
-- so editing or removing a past entry breaks every hash after it.
-- content_hash fingerprints exactly what was reviewed: the content
-- itself, as the reviewer saw it, is kept in listing_review_snapshots.
-- Names are copied in and nothing references other tables, so deleting
-- an organisation or a user never touches the log; a trigger refuses
-- updates and deletes. The chain's head is mailed to the platform's
-- admins daily (listing_review_anchors): a copy outside the database, so
-- even rewriting the whole log shows.
CREATE TABLE listing_review_log (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('event', 'party')),
    item_id      BIGINT NOT NULL,
    org_id       BIGINT,
    org_name     TEXT NOT NULL DEFAULT '',
    action       TEXT NOT NULL CHECK (action IN ('submitted', 'approved', 'rejected')),
    via          TEXT NOT NULL CHECK (via IN ('console', 'mcp', 'auto')),
    user_id      BIGINT,
    username     TEXT NOT NULL DEFAULT '',
    revision     TEXT NOT NULL DEFAULT '',
    graph_id     BIGINT,
    reason       TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL,
    prev_hash    TEXT NOT NULL,
    entry_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_listing_review_log_recent ON listing_review_log (created_at DESC);
CREATE INDEX idx_listing_review_log_item ON listing_review_log (kind, item_id, created_at DESC);

CREATE FUNCTION listing_review_log_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'listing_review_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER listing_review_log_append_only
    BEFORE UPDATE OR DELETE ON listing_review_log
    FOR EACH ROW EXECUTE FUNCTION listing_review_log_append_only();

-- What was reviewed, by content_hash: every text, the listing's fields,
-- and each picture, sound or file by its SHA-256.
CREATE TABLE listing_review_snapshots (
    content_hash TEXT PRIMARY KEY,
    content      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The chain heads mailed out, so the next mail only goes when it moved.
CREATE TABLE listing_review_anchors (
    id         BIGSERIAL PRIMARY KEY,
    head_hash  TEXT NOT NULL,
    entries    BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
