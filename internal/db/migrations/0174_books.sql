-- Books in the operator's shop (/shop): an order is a store purchase of
-- kind 'book', paid to the home organisation and posted to the buyer
-- like a store letter. It has no event, so it names its seller and the
-- book (product: the shop page's slug).
ALTER TABLE ticket_purchases
    ADD COLUMN seller_org_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    ADD COLUMN product       TEXT NOT NULL DEFAULT '',
    ADD COLUMN product_name  TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT ticket_purchases_kind_check,
    ADD CONSTRAINT ticket_purchases_kind_check CHECK (kind IN ('ticket', 'party', 'hosted', 'hosted_teams', 'book'));
