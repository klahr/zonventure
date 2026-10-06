-- The public quiz trail: anyone makes a trail on a phone with no account,
-- pays a hosted pass to play it, and runs it from the host link. A
-- trail is a hidden adventure of the seller org (the one with
-- quiz_trail_enabled) plus a trails row holding what the no-account door
-- needs: the draft's secret, the buyer's email, the hosted event once
-- paid. See admin/trails_public.go.

-- Whether an org sells quiz trails at /o/{slug}/trail. Set by a site
-- admin; no org-admin UI.
ALTER TABLE organizations
    ADD COLUMN quiz_trail_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- A trail made at the public door: kept out of the org's adventure
-- lists, the store, MCP and party offers. Cleared when the trail is
-- kept ("Keep it") and moves into the keeper's org.
ALTER TABLE adventures
    ADD COLUMN trail_hidden BOOLEAN NOT NULL DEFAULT FALSE;

-- One public trail. secret_hash is the SHA-256 of the draft's secret,
-- which only the maker's browser (and the link they copy) holds.
-- email is where the trail is saved, once confirmed (a confirm link, or
-- the payment). event_id is the hosted event and paid_at when it was
-- paid: a paid trail whose event is gone has ended. ip_hash
-- counts new drafts per address; keep_offer_sent_at records the day-7
-- mail. updated_at is the last edit: an unpaid draft goes 30 days on.
CREATE TABLE trails (
    id                  BIGSERIAL PRIMARY KEY,
    org_id              BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    adventure_id        BIGINT NOT NULL UNIQUE REFERENCES adventures(id) ON DELETE CASCADE,
    event_id            BIGINT REFERENCES events(id) ON DELETE SET NULL,
    paid_at             TIMESTAMPTZ,
    secret_hash         TEXT NOT NULL UNIQUE,
    email               TEXT,
    email_confirmed_at  TIMESTAMPTZ,
    ip_hash             TEXT NOT NULL DEFAULT '',
    keep_offer_sent_at  TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_trails_email ON trails (LOWER(email)) WHERE email_confirmed_at IS NOT NULL;
CREATE INDEX idx_trails_ip ON trails (ip_hash, created_at);
CREATE INDEX idx_trails_event ON trails (event_id) WHERE event_id IS NOT NULL;

-- Single-use mailed links and the sessions they open. 'confirm' saves
-- the trails it names (trail_ids) to email; 'signin' lists every trail
-- saved to email; using either gives the browser a 'session' (30 days)
-- that opens them all. 'keep' is the Keep it link of one trail, good
-- until its event lapses. Only hashes are stored.
CREATE TABLE trail_email_tokens (
    token_hash  TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    purpose     TEXT NOT NULL CHECK (purpose IN ('confirm', 'signin', 'session', 'keep')),
    trail_ids   BIGINT[] NOT NULL DEFAULT '{}',
    ip_hash     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);

CREATE INDEX idx_trail_email_tokens_email ON trail_email_tokens (LOWER(email), created_at);
CREATE INDEX idx_trail_email_tokens_expires ON trail_email_tokens (expires_at);
