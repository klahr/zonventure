-- Webhook delivery worker lease.
--
-- Earlier the worker held a Postgres row lock across the customer
-- HTTP call (up to 30 s). One slow-loris customer pinned a Postgres
-- backend the whole time and the queue serialised through a single
-- in-flight delivery — see internal/webhooks/delivery.go's prior
-- KNOWN LIMITATION comment.
--
-- The new shape splits the work into two transactions: tx1 claims a
-- row by stamping `in_flight_until` and committing (releasing the
-- lock), the customer HTTP call happens with no DB connection held,
-- tx2 writes the outcome. Stale leases (worker crashed mid-flight)
-- expire after the lease window and become eligible again on the
-- next tick.

ALTER TABLE webhook_deliveries
    ADD COLUMN in_flight_until TEXT;

-- Speed up the "due, not in flight" candidate query the worker runs
-- on every tick. Partial index keeps the size proportional to the
-- pending backlog rather than the lifetime of the table.
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending_due
    ON webhook_deliveries(next_attempt_at)
    WHERE status = 'pending';
