-- An organisation's contact address: the Reply-To of the mails it sends
-- buyers and teams, and the address they show. Empty is its first
-- admin's. Its time zone is the one start times are written in, in mails
-- and the results export.
ALTER TABLE organizations ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN time_zone TEXT NOT NULL DEFAULT 'Europe/Stockholm';
