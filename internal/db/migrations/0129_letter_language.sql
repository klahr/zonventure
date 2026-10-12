-- The language a gift box buyer's printed letter is posted in, chosen on
-- /redeem among the languages the product's adventure is played in.
-- Their emails are written in it too. Empty is from before the choice.
ALTER TABLE partner_redemptions ADD COLUMN letter_lang TEXT NOT NULL DEFAULT '';

-- The language a team starts playing in, copied into team_graph_state
-- when it is placed. Empty is the adventure's main language.
ALTER TABLE teams ADD COLUMN lang TEXT NOT NULL DEFAULT '';
