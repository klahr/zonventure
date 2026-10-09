-- A party can keep each game's results secret until its host finishes
-- the game: phones and the live board leave out points, places and the
-- quiz walk while it is played, so the host can reveal them together.
-- New parties hold them; parties made before keep showing them as they go.
ALTER TABLE events ADD COLUMN party_hold_results BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE events ALTER COLUMN party_hold_results SET DEFAULT TRUE;
