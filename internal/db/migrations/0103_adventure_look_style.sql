-- A look's finer choices (look.Style): card transparency now, more as
-- they come, in one object rather than a column each. NULL is defaults.
ALTER TABLE adventures ADD COLUMN look_style JSONB;
