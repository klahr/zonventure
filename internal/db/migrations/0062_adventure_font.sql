-- An adventure's typeface, from a short list the player knows how to load
-- (see adventure.Fonts). Empty is the Zonventure default.
ALTER TABLE adventures ADD COLUMN font TEXT NOT NULL DEFAULT '';
