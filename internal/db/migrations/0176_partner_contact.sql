-- Who to ask at a partner, and the portal its codes are checked in.
ALTER TABLE partners ADD COLUMN contact_name TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN portal_url TEXT NOT NULL DEFAULT '';
