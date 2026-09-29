-- Cards split into two choices: depth (cards = raised | flat) and a line
-- (card_outline). "outlined" was a third depth that was really a line.
ALTER TABLE adventures ADD COLUMN card_outline BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE adventures SET cards = 'flat', card_outline = TRUE WHERE cards = 'outlined';
