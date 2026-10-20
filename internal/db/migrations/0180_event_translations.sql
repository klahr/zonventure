-- An event's own words in the other languages its adventure is played
-- in (internal/listing): its name, store blurb and meeting place by
-- language tag, {"en": {"name": "…", "blurb": "…", "meeting_place": "…"}}.
-- The event keeps them as edited; a self-guided event's approved
-- listing keeps what a listing reviewer approved, as it keeps the
-- main-language name and blurb (listing review, 0123). The adventure's
-- own words are translations in the adventure version it plays.

ALTER TABLE events ADD COLUMN translations JSONB NOT NULL DEFAULT '{}';
ALTER TABLE event_listings ADD COLUMN translations JSONB NOT NULL DEFAULT '{}';

-- A file for the teams playing in one language, '' for every team
-- (eventfile.ListForPlayersIn).
ALTER TABLE event_files ADD COLUMN lang TEXT NOT NULL DEFAULT '';

-- An organiser's message in other languages, by language tag: a team
-- reads it in the language it plays in (eventmsg.In).
ALTER TABLE event_messages ADD COLUMN translations JSONB NOT NULL DEFAULT '{}';
