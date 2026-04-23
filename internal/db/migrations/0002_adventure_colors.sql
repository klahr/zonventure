-- Per-adventure branding: four optional hex colors so different adventures
-- (eg. Mind Race vs. a darker escape-room theme) can skin the play and
-- live views without touching code. NULL means "use the default theme";
-- empty string is normalised to NULL by the admin endpoints.
--
-- accent_color            — primary brand colour: buttons, progress fill
-- accent_secondary_color  — secondary brand colour: puzzle titles + the
--                           run-status pill, so the two accents alternate
--                           between "what to do" and "where you are"
-- background_color        — page background
-- text_color              — default ink colour (eg. pure black for a
--                           light-bg brand)
ALTER TABLE adventures ADD COLUMN accent_color TEXT;
ALTER TABLE adventures ADD COLUMN accent_secondary_color TEXT;
ALTER TABLE adventures ADD COLUMN background_color TEXT;
ALTER TABLE adventures ADD COLUMN text_color TEXT;
