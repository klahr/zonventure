-- area is where an adventure is played, in a few words for a store
-- card: "Stockholm · Frescati", "Göteborg · Slottsskogen", "Hemma".
ALTER TABLE adventures ADD COLUMN area TEXT NOT NULL DEFAULT '';
