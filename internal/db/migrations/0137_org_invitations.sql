-- Adding an existing account to an organisation asks that person
-- first: the row waits here until they accept (it becomes an
-- org_memberships row) or decline (it is deleted).
CREATE TABLE org_invitations (
    user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id             BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role               TEXT NOT NULL CHECK (role IN ('org_admin','org_member')),
    invited_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at         TEXT NOT NULL DEFAULT now_text(),
    PRIMARY KEY (user_id, org_id)
);
