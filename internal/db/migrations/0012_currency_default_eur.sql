-- Switch the purchases.currency default from 'USD' to 'EUR' so the
-- DB-level default matches the application's pricing in
-- internal/server/server.go (`auth.Pricing.Currency = "EUR"`). Every
-- application code path passes currency explicitly today, so this is
-- belt-and-suspenders against a future INSERT that forgets the
-- column. Existing rows are not rewritten — historical purchases
-- keep whatever currency they were originally charged in.
ALTER TABLE purchases ALTER COLUMN currency SET DEFAULT 'EUR';
