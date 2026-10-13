-- The address an existing account was invited at. Settings → Members
-- lists pending invitations by it: the org's admins typed that address,
-- the account's username they never learn.
ALTER TABLE org_invitations ADD COLUMN email TEXT;
