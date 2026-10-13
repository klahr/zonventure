-- Where teams meet, in the organiser's words: said in the invitation and
-- the calendar entry's LOCATION.
ALTER TABLE events ADD COLUMN meeting_place TEXT NOT NULL DEFAULT '';
