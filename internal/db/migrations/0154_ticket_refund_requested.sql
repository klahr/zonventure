-- A refund asked of the payment provider and not yet confirmed by its
-- charge.refunded webhook: the sale shows as refund pending, and a
-- second refund of it is refused. Cleared when the provider call fails.
ALTER TABLE ticket_purchases ADD COLUMN refund_requested_at TEXT;
