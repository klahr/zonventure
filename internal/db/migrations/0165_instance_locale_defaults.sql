-- An organisation's time zone and ship countries default to the
-- instance's (ZV_TIME_ZONE, ZV_SHIP_COUNTRIES): empty means those.
-- Rows that have a value keep it.
ALTER TABLE organizations ALTER COLUMN time_zone SET DEFAULT '';
ALTER TABLE organizations ALTER COLUMN ship_countries SET DEFAULT '{}';
ALTER TABLE organizations DROP CONSTRAINT organizations_ship_countries_check;
