-- Multi-org rollout, phase 4 — drop the legacy single-org pin from
-- users. After phases 1-3, org_memberships is the source of truth
-- for per-org roles and sessions.active_org_id is the per-request
-- selection. users.role and users.org_id are now vestigial.
--
-- Admins (the global tier with no org context) used to be identified
-- by users.role = 'admin'. The replacement signal is users.is_admin —
-- a plain boolean, since "admin" doesn't fit the per-org role model
-- anyway. Backfilled from the legacy role before the column drop.

ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users SET is_admin = TRUE WHERE role = 'admin';

-- CASCADE drops every dependency in one stroke: the inline column
-- CHECK on role, the (role, org_id) table-level CHECK, the FK from
-- org_id to organizations, and idx_users_org.
ALTER TABLE users DROP COLUMN role CASCADE;
ALTER TABLE users DROP COLUMN org_id CASCADE;
