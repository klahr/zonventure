-- A pattern behind the cards (adventure.Backdrop): its kind and settings,
-- never CSS. NULL is the plain page colour.
ALTER TABLE adventures ADD COLUMN backdrop JSONB;
