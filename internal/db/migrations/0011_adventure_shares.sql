-- Admins can commission an adventure for a specific organisation:
-- targeted visibility without going through the public Store. A share
-- grants the org the right to see the adventure on their Adventures
-- page and schedule runs directly (no assignment, no max_teams ceiling).
-- Revoking a share doesn't touch runs already created from it.
--
-- adventure_slug is a free text column here (no FK), matching how
-- adventure_assignments holds the slug — this preserves the in-app
-- slug-rename flow without needing ON UPDATE CASCADE gymnastics.
-- Dangling shares are cleaned up in DeleteAdventure alongside
-- assignments.
CREATE TABLE adventure_shares (
    adventure_slug      TEXT NOT NULL,
    org_id              INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    granted_by_user_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    granted_at          TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (adventure_slug, org_id)
);

CREATE INDEX idx_shares_org ON adventure_shares(org_id);
