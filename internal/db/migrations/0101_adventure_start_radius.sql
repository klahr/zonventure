-- Optionally require teams to begin at the adventure's starting point.
-- When start_radius_m is set (with start_lat / start_lng), a team is
-- held at the start screen until one of its players is within that many
-- metres of the starting point, and only then can it begin, so the clock
-- starts where the adventure does. NULL means no requirement: the
-- starting point is only shown as where to meet, as before.
ALTER TABLE adventures ADD COLUMN start_radius_m DOUBLE PRECISION;
