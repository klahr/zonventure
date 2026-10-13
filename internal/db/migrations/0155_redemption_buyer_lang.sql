-- The language a buyer redeemed a gift-box code in, on the public form;
-- the mails about it are written in it, as a store buyer's ticket mail
-- is (ticket_purchases.buyer_lang). Empty is from before, or entered by
-- an organiser: the mails then follow the posted letter.
ALTER TABLE partner_redemptions ADD COLUMN buyer_lang TEXT NOT NULL DEFAULT '';
