-- Three theme presets beside the colours and font: the heading typeface
-- (from the same short list as font), the corner style and the card
-- style. Presets, not values, so every combination stays legible and
-- the designer's preview matches the player. Empty is today's look.
ALTER TABLE adventures ADD COLUMN heading_font TEXT NOT NULL DEFAULT '';
ALTER TABLE adventures ADD COLUMN corners TEXT NOT NULL DEFAULT '';
ALTER TABLE adventures ADD COLUMN cards TEXT NOT NULL DEFAULT '';
