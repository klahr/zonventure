-- Add a top_up_assignment_id column to purchases so a buy can target an
-- existing assignment instead of minting a new one. NULL keeps the
-- legacy "create assignment on confirm" semantics; non-NULL means
-- "this purchase tops up the named assignment's max_players cap".
--
-- ON DELETE SET NULL so a purchases row stays around (paid history)
-- even if the operator later deletes the assignment it bumped.

ALTER TABLE purchases
  ADD COLUMN top_up_assignment_id BIGINT REFERENCES adventure_assignments(id) ON DELETE SET NULL;

CREATE INDEX idx_purchases_topup ON purchases(top_up_assignment_id)
  WHERE top_up_assignment_id IS NOT NULL;
