-- buyer_note is the author's word to whoever buys the adventure: added to
-- the ticket and party-host emails, never shown to players.
ALTER TABLE adventures ADD COLUMN buyer_note TEXT NOT NULL DEFAULT '';
