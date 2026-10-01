-- A roles block can make a team's roles final as it leaves the step
-- (graph.BlockRoles, Lock): what was dealt is stored in team_roles then,
-- and from here no one takes or gives back a role, nor is any dealt again.
ALTER TABLE teams ADD COLUMN roles_locked BOOLEAN NOT NULL DEFAULT FALSE;
