-- The language a store buyer browsed in when they paid; their ticket
-- email is written in it. Empty is from before, or a purchase made
-- elsewhere: the email then follows the posted letter or the adventure.
ALTER TABLE ticket_purchases ADD COLUMN buyer_lang TEXT NOT NULL DEFAULT '';
