-- Gift boxes sold through experience partners (Live it, TrueStory). A
-- buyer redeems the box's code on /redeem; an operator checks the code
-- in the partner's own portal and approves or rejects the request.
-- Approval creates a team on the product's self-guided event, the same
-- way a paid /store ticket does.
--
-- partners: which partners an organisation takes boxes from. kind
-- fixes the code format: Live it boxes carry a box number and a
-- security code, TrueStory boxes a 9-character experience code.
CREATE TABLE partners (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('liveit', 'truestory')),
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TEXT NOT NULL DEFAULT now_text(),
    UNIQUE (org_id, kind)
);

-- partner_products: what a box can be redeemed for. The buyer picks one
-- by name; approval creates a team of team_size on event_id.
CREATE TABLE partner_products (
    id          BIGSERIAL PRIMARY KEY,
    partner_id  BIGINT NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    team_size   INTEGER NOT NULL CHECK (team_size BETWEEN 1 AND 50),
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_partner_products_partner ON partner_products(partner_id);

-- partner_redemptions: one request per box. The postal address, post_at
-- (when to send it) and posted_at track what is mailed to the buyer.
-- security_code is stored normalised (upper case, no spaces or dashes).
-- product_name keeps what was redeemed once the product (or its event)
-- is deleted; the request itself goes only with its organisation.
CREATE TABLE partner_redemptions (
    id                  BIGSERIAL PRIMARY KEY,
    partner_id          BIGINT NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
    product_id          BIGINT REFERENCES partner_products(id) ON DELETE SET NULL,
    product_name        TEXT NOT NULL,
    kind                TEXT NOT NULL,
    box_number          TEXT NOT NULL DEFAULT '',
    security_code       TEXT NOT NULL,
    name                TEXT NOT NULL,
    email               TEXT NOT NULL,
    street_address      TEXT NOT NULL,
    address_2           TEXT NOT NULL DEFAULT '',
    postal_code         TEXT NOT NULL,
    city                TEXT NOT NULL,
    state               TEXT NOT NULL DEFAULT 'pending'
                        CHECK (state IN ('pending', 'approved', 'rejected', 'cancelled')),
    team_id             BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    post_at             TEXT NOT NULL DEFAULT '',
    posted_at           TEXT,
    note                TEXT NOT NULL DEFAULT '',
    created_by_user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    decided_by_user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    decided_at          TEXT,
    created_at          TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_partner_redemptions_partner_state ON partner_redemptions(partner_id, state);

-- A partner's code is redeemable once, across every organisation that
-- takes that partner's boxes. A rejected or cancelled request frees it.
CREATE UNIQUE INDEX idx_partner_redemptions_code
    ON partner_redemptions(kind, box_number, security_code)
    WHERE state IN ('pending', 'approved');
