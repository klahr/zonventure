-- Parties: a one-off private adventure a parent books and pays for with
-- no account. The seller org offers some of its adventures as parties;
-- a paid booking becomes an ordinary event of the seller's, run from a
-- host page that a secret link opens, and deleted a while after it ends.

-- What an org sells as a party: one row per adventure it offers. The
-- adventure is the org's own or one shared with it. Price is a base plus
-- a charge per child, in minor units.
CREATE TABLE party_offers (
    id              BIGSERIAL PRIMARY KEY,
    org_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    adventure_slug  TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    base_cents      INTEGER NOT NULL DEFAULT 0 CHECK (base_cents >= 0),
    per_kid_cents   INTEGER NOT NULL DEFAULT 0 CHECK (per_kid_cents >= 0),
    currency        TEXT NOT NULL DEFAULT 'sek',
    max_kids        INTEGER NOT NULL DEFAULT 20 CHECK (max_kids BETWEEN 1 AND 50),
    blurb           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, adventure_slug)
);

-- A party event. host_token_hash is the SHA-256 of the host link's
-- secret: the link is the host's only credential, so the database never
-- holds it in the clear. party_kids caps the players the host may add.
-- party_mode is how zones are played: 'qr' as cards the host hides
-- (party_stations), 'park' as places outdoors. party_overrides is what
-- the host changed of the adventure (graph.PartyOverrides): their
-- questions, their words, where they put its places.
-- valid_until is when an unstarted party lapses. The retention sweeper
-- deletes a party some days after it ends, or once it has lapsed.
ALTER TABLE events
    ADD COLUMN party            BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN host_token_hash  TEXT UNIQUE,
    ADD COLUMN party_name       TEXT NOT NULL DEFAULT '',
    ADD COLUMN party_kids       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN party_mode       TEXT NOT NULL DEFAULT 'qr' CHECK (party_mode IN ('qr', 'park')),
    ADD COLUMN party_overrides  JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN valid_until      TIMESTAMPTZ;

CREATE INDEX idx_events_party ON events (state) WHERE party;

-- QR mode: a party plays its adventure's zones as stations, cards the
-- host prints and hides. Each zone gets a code of its own per party, so
-- a code from another party, or from the adventure's author, opens
-- nothing here.
CREATE TABLE party_stations (
    event_id   BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    zone_slug  TEXT NOT NULL,
    code       TEXT NOT NULL,
    PRIMARY KEY (event_id, zone_slug),
    UNIQUE (event_id, code)
);

-- The station a team scanned last: in QR mode that is where the team is.
ALTER TABLE teams
    ADD COLUMN station_zone TEXT,
    ADD COLUMN station_at   TIMESTAMPTZ;

-- A party booking rides the ticket purchase flow. Its event is made
-- when the payment lands, so until then the row names the offer
-- instead. team_size holds the number of children; consent_at records
-- the booker's guardian declaration.
ALTER TABLE ticket_purchases
    ADD COLUMN kind            TEXT NOT NULL DEFAULT 'ticket' CHECK (kind IN ('ticket', 'party')),
    ADD COLUMN party_offer_id  BIGINT REFERENCES party_offers(id) ON DELETE SET NULL,
    ADD COLUMN party_name      TEXT NOT NULL DEFAULT '',
    ADD COLUMN consent_at      TIMESTAMPTZ;
