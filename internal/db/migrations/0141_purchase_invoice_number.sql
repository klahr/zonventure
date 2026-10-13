-- The number of the off-platform invoice an invoice purchase was
-- billed on, entered by the operator with "Mark billed" and shown to
-- the org beside the purchase.
ALTER TABLE purchases ADD COLUMN invoice_number TEXT NOT NULL DEFAULT '';
