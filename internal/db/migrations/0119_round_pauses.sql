-- A party's live board times each game on its own: paused_from is the
-- event's paused_seconds when the game opened, and paused_seconds what
-- the game itself spent paused, set when it finishes. NULL for games
-- opened before, which count no pauses.
ALTER TABLE event_rounds ADD COLUMN paused_from INTEGER;
ALTER TABLE event_rounds ADD COLUMN paused_seconds INTEGER;
