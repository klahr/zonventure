-- A writer authors the organisation's adventures and nothing else: no
-- events, members, billing or store (admin.writerMay, canAuthor).
ALTER TABLE org_memberships DROP CONSTRAINT org_memberships_role_check;
ALTER TABLE org_memberships ADD CONSTRAINT org_memberships_role_check
    CHECK (role IN ('org_admin', 'org_member', 'org_writer'));
ALTER TABLE org_invitations DROP CONSTRAINT org_invitations_role_check;
ALTER TABLE org_invitations ADD CONSTRAINT org_invitations_role_check
    CHECK (role IN ('org_admin', 'org_member', 'org_writer'));
