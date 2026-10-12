-- Store purchases whose adventure posts a letter: the buyer picks the
-- letter's language before checkout, and checkout collects where to
-- post it. post_at, posted_at and note track the posting, as on
-- partner_redemptions.
ALTER TABLE ticket_purchases
    ADD COLUMN letter_lang    TEXT NOT NULL DEFAULT '',
    ADD COLUMN ship_name      TEXT NOT NULL DEFAULT '',
    ADD COLUMN street_address TEXT NOT NULL DEFAULT '',
    ADD COLUMN address_2      TEXT NOT NULL DEFAULT '',
    ADD COLUMN postal_code    TEXT NOT NULL DEFAULT '',
    ADD COLUMN city           TEXT NOT NULL DEFAULT '',
    ADD COLUMN country        TEXT NOT NULL DEFAULT '',
    ADD COLUMN post_at        TEXT NOT NULL DEFAULT '',
    ADD COLUMN posted_at      TEXT,
    ADD COLUMN note           TEXT NOT NULL DEFAULT '';

-- Where an organisation posts letters: ISO 3166-1 alpha-2 codes.
ALTER TABLE organizations ADD COLUMN ship_countries TEXT[] NOT NULL DEFAULT '{SE}'
    CHECK (cardinality(ship_countries) > 0);

-- Boxes so far were all posted within Sweden.
ALTER TABLE partner_redemptions ADD COLUMN country TEXT NOT NULL DEFAULT '';
UPDATE partner_redemptions SET country = 'SE';
