-- The teams a full game turned away, by name (lowercased, so a team
-- that tries again counts once), for the host page to say how many
-- couldn't join. Deleted with the round.
ALTER TABLE event_rounds ADD COLUMN turned_away TEXT[] NOT NULL DEFAULT '{}';
