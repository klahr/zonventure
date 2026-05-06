-- Always-on, public-for-sale events. Today every event is bought by
-- an arranger (one purchases row per org per adventure run) and is
-- bound to a scheduled_at. The store flow is the inverse: an
-- operator stands up an event without a scheduled_at, manually
-- starts it, flips public_for_sale=true and sets a ticket price.
-- The event then surfaces on the public /store page; a player
-- clicks "Buy", pays via Stripe Checkout, and the webhook creates a
-- single team in the event and emails the buyer a magic link.
--
-- Five columns on events:
--   public_for_sale    — listed on /store?
--   ticket_price_cents — minor units; required when public_for_sale.
--   ticket_currency    — ISO 4217 lower-case ('eur', 'usd', 'sek').
--                        Default mirrors createStripeCheckoutSession.
--   ticket_team_size   — slots auto-created per ticket. Default 5 so
--                        a buyer can use the existing team-share QR
--                        to bring up to four friends without a
--                        size-picker at checkout.
--   public_blurb       — store-card teaser; the adventure name
--                        becomes the card title above it.
ALTER TABLE events
    ADD COLUMN public_for_sale     BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN ticket_price_cents  INTEGER,
    ADD COLUMN ticket_currency     TEXT NOT NULL DEFAULT 'eur',
    ADD COLUMN ticket_team_size    INTEGER NOT NULL DEFAULT 5
        CHECK (ticket_team_size BETWEEN 1 AND 50),
    ADD COLUMN public_blurb        TEXT NOT NULL DEFAULT '';

-- Partial index: the listing query is the only read path on this
-- column, and almost every event has it FALSE.
CREATE INDEX idx_events_public_for_sale ON events(public_for_sale)
    WHERE public_for_sale;

-- Ticket purchases: one row per "buy a ticket" Stripe checkout. Kept
-- separate from `purchases` (which bills orgs for adventure usage)
-- because the audiences, lifecycle, and refund posture differ — a
-- ticket lives or dies with its event, no adventure_assignments row
-- is ever created, the buyer is a third party with no org or login.
CREATE TABLE ticket_purchases (
    id                   BIGSERIAL PRIMARY KEY,
    event_id             BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    buyer_email          TEXT NOT NULL,
    team_size            INTEGER NOT NULL CHECK (team_size BETWEEN 1 AND 50),
    amount_cents         INTEGER NOT NULL,
    currency             TEXT NOT NULL,
    state                TEXT NOT NULL CHECK (state IN ('pending','paid','failed','refunded')) DEFAULT 'pending',
    team_id              BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    stripe_session_id    TEXT,
    stripe_charge_id     TEXT,
    stripe_invoice_url   TEXT,
    created_at           TEXT NOT NULL DEFAULT now_text(),
    paid_at              TEXT,
    refunded_at          TEXT
);

CREATE INDEX idx_ticket_purchases_event ON ticket_purchases(event_id);
CREATE INDEX idx_ticket_purchases_state ON ticket_purchases(state);
-- One Stripe checkout session id per purchase. The webhook uses this
-- to dedupe redeliveries that race with the metadata-keyed lookup.
CREATE UNIQUE INDEX idx_ticket_purchases_session
    ON ticket_purchases(stripe_session_id)
    WHERE stripe_session_id IS NOT NULL;
