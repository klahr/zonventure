-- The group of a team split up (team_graph_state.strands) the phone was
-- walking as it sent a ping, as the forks it came down (tracks.Tag);
-- NULL for a phone walking none, or a team together. A group's
-- trail is drawn from its own pings, so it keeps apart from the team's
-- before the split and after the groups meet again.
ALTER TABLE location_pings ADD COLUMN groups TEXT[];
