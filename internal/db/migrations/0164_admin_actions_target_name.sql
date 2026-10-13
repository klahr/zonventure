-- What an admin action's route ids named when it ran ("Acme · Summer
-- quest"), so Settings → Audit reads without looking ids up; empty on
-- the rows from before.
ALTER TABLE admin_actions ADD COLUMN target_name TEXT NOT NULL DEFAULT '';
