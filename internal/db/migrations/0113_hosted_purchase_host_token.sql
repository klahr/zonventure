-- A hosted purchase's host link secret, minted at checkout so the
-- browser that pays can open the admin page as soon as the payment
-- lands, instead of waiting for the mail. The webhook makes the event
-- with this secret (hashed there, as always) and clears it here once the
-- mail is sent; a purchase never paid keeps it until it is purged.
ALTER TABLE ticket_purchases ADD COLUMN host_token TEXT;
