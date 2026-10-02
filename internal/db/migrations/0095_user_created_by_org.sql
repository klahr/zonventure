-- Which org created this account, when it was made through an org's
-- "add member" flow (CreateOrgMember new-user branch). NULL for every
-- account a person made themselves (self-signup, the seeded admin) and
-- for every account that predates this column.
--
-- It is the gate for org-admin credential operations: an org_admin may
-- reset a member's password or clear their TOTP only for accounts their
-- own org created AND that have not since joined another org. Without
-- this, an org_admin could pull any existing account into their org and
-- reset its credentials, taking over a login a person uses elsewhere.
ALTER TABLE users
    ADD COLUMN created_by_org_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
