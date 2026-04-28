-- Multi-org support, phase 1 — schema only.
--
-- Until now `users.org_id` pinned each arranger to exactly one
-- organisation. Phase 1 introduces the canonical `org_memberships`
-- table so a user can belong to several orgs (potentially with
-- different roles in each), plus `sessions.active_org_id` so the
-- session carries which membership is currently selected.
--
-- `users.org_id` and `users.role` stay populated as the "primary"
-- membership for rollback safety while phases 2 (handler migration)
-- and 3 (UI) ship. Phase 4 drops both columns once the cutover is
-- proven in production.

CREATE TABLE org_memberships (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('org_admin','org_member')),
    created_at TEXT NOT NULL DEFAULT now_text(),
    PRIMARY KEY (user_id, org_id)
);

-- Reverse lookup: "list every member of org X" is the org-detail
-- page's hot path. Without this the lookup fans out across every
-- membership row.
CREATE INDEX idx_org_memberships_org ON org_memberships(org_id);

-- Seed one membership per existing arranger from the legacy single
-- pin. Admins (role = 'admin', org_id IS NULL by table CHECK) are
-- naturally skipped — they're not org-scoped at all.
INSERT INTO org_memberships (user_id, org_id, role)
SELECT id, org_id, role
FROM users
WHERE org_id IS NOT NULL AND role IN ('org_admin','org_member');

-- Active-org pin per session. NULL is a valid state: admin sessions
-- (no org context) and any session opened before this migration
-- ran but not touched by the backfill below. The auth middleware
-- treats NULL as "fall back to the user's only membership, or force
-- a re-pick if they belong to several".
ALTER TABLE sessions ADD COLUMN active_org_id BIGINT
    REFERENCES organizations(id) ON DELETE SET NULL;

-- Backfill arranger sessions so a logged-in user doesn't get bounced
-- to the org-picker on the next page load just because the migration
-- ran. Admin sessions stay NULL.
UPDATE sessions
SET active_org_id = u.org_id
FROM users u
WHERE sessions.user_id = u.id
  AND u.org_id IS NOT NULL;
