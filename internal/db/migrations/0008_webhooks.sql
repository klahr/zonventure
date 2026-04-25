-- Outbound webhook subscriptions and durable delivery queue.
--
-- Per-org endpoints subscribe to a subset of event kinds; whenever a
-- subscribed event fires we insert one row into webhook_deliveries
-- per matching endpoint and the delivery worker drains it with
-- exponential backoff. Two-table split (config vs. queue) keeps the
-- worker's hot path indexed on (status, next_attempt_at) without
-- cluttering the config table.

CREATE TABLE webhook_endpoints (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The destination URL the customer wants events POSTed to. We
    -- enforce https in the handler — storing as TEXT here so a
    -- future "internal http during dev" escape hatch costs no
    -- migration.
    url          TEXT   NOT NULL,
    -- HMAC-SHA256 secret. Returned ONCE on create — never again from
    -- list/get endpoints. Stored as random bytes hex-encoded; the
    -- customer's verification side hashes the raw body with this key
    -- and compares to the X-Zv-Signature header.
    secret       TEXT   NOT NULL,
    -- JSONB array of subscribed kinds; an empty array means
    -- "subscribe to nothing" rather than "all" so a half-configured
    -- row can't fan out everything by accident. JSONB (rather than
    -- TEXT[]) so the database/sql + pgx stdlib driver can scan it
    -- back into []string via json.Unmarshal — matches the existing
    -- "JSON-as-text" pattern in run_events.payload.
    event_kinds  JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Soft-disable so the customer can pause without deleting.
    active       BOOLEAN NOT NULL DEFAULT TRUE,
    -- Free-form name to disambiguate when an org has several
    -- endpoints (eg. "prod-slack", "staging-zapier").
    name         TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT now_text()
);

CREATE INDEX idx_webhook_endpoints_org ON webhook_endpoints(org_id);

-- Each row represents one attempted delivery of one event to one
-- endpoint. Status transitions: pending → delivered (terminal) OR
-- pending → pending (after a failed attempt with backoff applied)
-- OR pending → dead (terminal, after max attempts).
CREATE TABLE webhook_deliveries (
    id              BIGSERIAL PRIMARY KEY,
    endpoint_id     BIGINT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    -- Stable id we put in X-Zv-Delivery-Id so customers can dedupe.
    -- Not the row id (which is internal) so we can rotate the
    -- internal id space later if we change the storage shape.
    delivery_uuid   TEXT NOT NULL UNIQUE,
    event_kind      TEXT NOT NULL,
    -- The full JSON body we'll POST. Stored verbatim so retries send
    -- the exact same bytes the original signature was over —
    -- recomputing on every send would invalidate signatures if the
    -- payload shape ever shifts mid-retry.
    payload         TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending','delivered','dead')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL DEFAULT now_text(),
    last_error      TEXT,
    last_status     INTEGER, -- HTTP status from the most recent attempt, if any
    created_at      TEXT NOT NULL DEFAULT now_text(),
    delivered_at    TEXT
);

-- Hot path: the worker selects pending rows whose next_attempt_at
-- has arrived. Partial index keeps it small — delivered/dead rows
-- pile up over time but don't bloat this index.
CREATE INDEX idx_webhook_deliveries_due
    ON webhook_deliveries(next_attempt_at)
    WHERE status = 'pending';

CREATE INDEX idx_webhook_deliveries_endpoint
    ON webhook_deliveries(endpoint_id, created_at DESC);
