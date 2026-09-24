-- Delivery bookkeeping for graph effects, which until now were queued
-- and never sent. A webhook is retried with backoff and given up on
-- after a few attempts, as org webhooks are (webhook_deliveries).
ALTER TABLE graph_effect_outbox
    ADD COLUMN attempts        INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN last_status     INTEGER,
    ADD COLUMN last_error      TEXT,
    ADD COLUMN dead_at         TIMESTAMPTZ;

CREATE INDEX idx_graph_effect_outbox_due
    ON graph_effect_outbox (next_attempt_at)
    WHERE delivered_at IS NULL AND dead_at IS NULL;
