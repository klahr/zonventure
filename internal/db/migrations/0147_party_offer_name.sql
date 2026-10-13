-- A party offer's own name on the public page and at booking, so an
-- organisation can market it apart from the adventure. Empty means the
-- adventure's title. The approved copy keeps what review approved.
ALTER TABLE party_offers ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE party_offer_listings ADD COLUMN name TEXT NOT NULL DEFAULT '';
