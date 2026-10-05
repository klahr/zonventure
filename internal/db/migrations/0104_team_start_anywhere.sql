-- An arranger's word that a team may begin wherever it stands: the
-- adventure's required starting point is not asked of it (a phone whose
-- GPS will not settle, a meeting point moved on the day).
ALTER TABLE teams ADD COLUMN start_anywhere BOOLEAN NOT NULL DEFAULT FALSE;
