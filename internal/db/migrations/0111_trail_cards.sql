-- Quiz trails: an adventure written in the simple creator as an ordered
-- list of cards (internal/trail), from which its graph is made, one way.
-- trail_cards is NULL for an adventure made in the designer, and set
-- back to NULL when a trail is opened in the designer for good. (The
-- theme's "cards" column is the look of a step's cards; unrelated.)
--
-- trail_revision counts saves, so two tabs can't overwrite each other.
-- trail_graph is the fingerprint (graphstore.Fingerprint) of the draft
-- the creator last made: a draft that no longer matches was changed in
-- the designer, and the creator leaves it alone. trail_printed is the
-- revision last printed. trail_template and trail_kids are what the
-- trail was started from; kids asks a buyer for a guardian's consent.
ALTER TABLE adventures
    ADD COLUMN trail_cards     JSONB,
    ADD COLUMN trail_revision  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN trail_graph     TEXT NOT NULL DEFAULT '',
    ADD COLUMN trail_printed   INTEGER,
    ADD COLUMN trail_template  TEXT NOT NULL DEFAULT '',
    ADD COLUMN trail_kids      BOOLEAN NOT NULL DEFAULT FALSE;
