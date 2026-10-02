-- An optional passphrase a team must enter to begin an adventure. When
-- set, the team is held at the start screen (and, for a single-start
-- adventure, is NOT auto-advanced when the event starts) until someone
-- types the code — so the clock only starts once they actually begin.
-- NULL / empty means no gate: the adventure starts as before.
--
-- Free text, matched case-insensitively after trimming (see
-- session.ChooseStart). Distinct from cheat_password, which skips
-- puzzles mid-game rather than gating the start.
ALTER TABLE adventures ADD COLUMN start_code TEXT;
