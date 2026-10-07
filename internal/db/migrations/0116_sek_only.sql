-- The platform and its orgs sell in SEK only (PLATFORM_PRICING.md). New
-- rows default to it; an event already priced in another currency keeps
-- it until its price is next saved, which saves it in SEK.
ALTER TABLE events ALTER COLUMN ticket_currency SET DEFAULT 'sek';
ALTER TABLE purchases ALTER COLUMN currency SET DEFAULT 'SEK';
