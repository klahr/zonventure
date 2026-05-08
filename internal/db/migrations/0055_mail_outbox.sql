-- Durable mail outbox for transactional sends that absolutely must
-- reach the recipient — primarily the post-Stripe ticket-fulfilment
-- magic link.
--
-- Earlier the post-paid handler called Mailer.Send synchronously. A
-- failure (SMTP outage, recipient bounce, transient DNS hiccup) was
-- logged but the buyer's purchase still committed: paid-but-stranded
-- with no UI to trigger a resend. The outbox decouples the two —
-- the handler enqueues a row and commits; a worker drains the
-- outbox with retry + backoff.
--
-- Attachments aren't modelled today. The two callers that send
-- attachments (admin/invitations.go for ICS, player/recover.go for
-- magic-link recovery) keep their direct Mailer.Send calls; they're
-- fired in interactive flows where a failure is recoverable from
-- the UI. The outbox is for the load-bearing post-Stripe path
-- where the user has already paid.

CREATE TABLE mail_outbox (
    id              SERIAL PRIMARY KEY,
    -- kind labels the message family for telemetry / retention.
    -- "ticket_team_link" today; future kinds added by callers.
    kind            TEXT NOT NULL,
    to_addr         TEXT NOT NULL,
    subject         TEXT NOT NULL,
    body_text       TEXT NOT NULL DEFAULT '',
    body_html       TEXT NOT NULL DEFAULT '',
    reply_to        TEXT,
    -- attempts increments on every claim, with or without a successful
    -- outcome write — same shape as webhook_deliveries.attempts so a
    -- crashed worker can't retry forever.
    attempts        INTEGER NOT NULL DEFAULT 0,
    -- status: pending → sent (success) or dead (max attempts reached).
    -- A pending row whose in_flight_until is in the past is eligible
    -- for re-claim by another worker.
    status          TEXT NOT NULL DEFAULT 'pending',
    next_attempt_at TEXT NOT NULL DEFAULT (now()::text),
    in_flight_until TEXT,
    last_err        TEXT,
    created_at      TEXT NOT NULL DEFAULT (now()::text),
    sent_at         TEXT
);

-- Worker selects the oldest pending+due row on every tick. Partial
-- index keeps it scoped to the work the worker actually does.
CREATE INDEX idx_mail_outbox_pending_due
    ON mail_outbox(next_attempt_at)
    WHERE status = 'pending';
