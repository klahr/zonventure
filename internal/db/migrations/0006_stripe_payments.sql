-- Stripe integration adds three columns to purchases for the
-- payments-side bookkeeping the webhook needs to match events back
-- to a row, plus a side table for webhook-event idempotency. The
-- existing provider_session_id is reused as Stripe's Checkout
-- Session id (cs_test_… or cs_live_…); only the post-payment
-- charge id and hosted-invoice URL are new.
ALTER TABLE purchases ADD COLUMN stripe_charge_id TEXT;
ALTER TABLE purchases ADD COLUMN stripe_invoice_url TEXT;

-- Stripe retries delivery on any non-2xx response and may also
-- double-deliver during region failovers. processed_webhook_events
-- holds every event id we've already acted on so a re-delivery is a
-- no-op rather than a duplicate assignment-creation. The received_at
-- column is purely diagnostic — useful when a customer asks "did the
-- payment happen?" and you want to correlate without round-tripping
-- to Stripe.
CREATE TABLE processed_webhook_events (
    event_id    TEXT PRIMARY KEY,
    received_at TEXT NOT NULL DEFAULT now_text()
);
