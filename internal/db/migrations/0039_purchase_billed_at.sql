-- Track which off-platform invoice purchases the operator has already
-- billed. NULL = not yet invoiced (shows up in the "to bill" filter
-- on the admin Purchases page); non-NULL = the operator clicked
-- "Mark billed" after sending the real invoice from outside the
-- platform.

ALTER TABLE purchases ADD COLUMN billed_at TEXT;
