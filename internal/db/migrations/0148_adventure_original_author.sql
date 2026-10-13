-- Who first wrote an imported adventure: its author becomes the importing
-- org, and this keeps the credit.
ALTER TABLE adventures ADD COLUMN original_author TEXT NOT NULL DEFAULT '';
