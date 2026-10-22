-- facts are what a buyer weighs before buying an adventure, shown in the
-- store: {minutes, distance_m, min_age, difficulty, access}, each left
-- out when unknown (adventure.Facts). access_note says what access alone
-- can't ("Stairs at the fourth stop"), translated and reviewed as area is.
ALTER TABLE adventures ADD COLUMN facts JSONB NOT NULL DEFAULT '{}';
ALTER TABLE adventures ADD COLUMN access_note TEXT NOT NULL DEFAULT '';
