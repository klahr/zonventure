-- Pay per team (PLATFORM_PRICING.md). Every team that plays an
-- organisation's event costs it a price, its first phones included and
-- each further phone a price of its own, charged when the team first
-- joins a game and when a phone first opens its seat. What isn't paid
-- in a store payment is taken from the organisation's balance: one
-- running sum of what it put in (top-ups, their bonus, monthly bills
-- paid) and what its teams and phones cost.
--
-- A prepaid organisation may go a little below zero (the overdraft); a
-- monthly one down to its credit limit, and is billed on the 1st for
-- what brings it back to zero, by card or by invoice.

-- billing: 'teams', every team charged to the organisation; 'prepaid',
-- paid at checkout (a family's party, a party offer bought in the
-- store), nothing charged. Dev events are never charged either.
ALTER TABLE events
    ADD COLUMN billing TEXT NOT NULL DEFAULT 'teams' CHECK (billing IN ('teams', 'prepaid'));
UPDATE events e SET billing = 'prepaid'
WHERE e.party AND (EXISTS (SELECT 1 FROM trails t WHERE t.event_id = e.id)
                OR EXISTS (SELECT 1 FROM ticket_purchases p WHERE p.event_id = e.id AND p.kind = 'party'));

-- paid_phones: a team bought in the store, with this many phones paid
-- for in its ticket; it costs nothing more until a phone past them (and
-- past the phones a team includes) opens its seat. NULL: not paid.
ALTER TABLE teams ADD COLUMN paid_phones SMALLINT;
UPDATE teams t SET paid_phones = p.team_size
FROM ticket_purchases p WHERE p.team_id = t.id AND p.kind = 'ticket' AND p.state = 'paid';

ALTER TABLE organizations
    ADD COLUMN billing_mode TEXT NOT NULL DEFAULT 'prepaid' CHECK (billing_mode IN ('prepaid', 'monthly')),
    -- A platform admin's credit limit for this organisation; NULL follows
    -- the platform's (a new one's, grown after enough bills are paid).
    ADD COLUMN credit_limit_cents BIGINT CHECK (credit_limit_cents >= 0),
    -- The card a monthly organisation is charged on: its Stripe payment
    -- method, and how it is shown ("Visa •••• 4242").
    ADD COLUMN billing_payment_method TEXT NOT NULL DEFAULT '',
    ADD COLUMN billing_card_label TEXT NOT NULL DEFAULT '',
    -- The last balance warning mailed to its admins: 'low' (room for
    -- few more teams) or 'empty'; '' once topped up past them.
    ADD COLUMN billing_alert TEXT NOT NULL DEFAULT '' CHECK (billing_alert IN ('', 'low', 'empty'));

-- A monthly organisation's bill for a month: what brought its balance
-- back to zero then. Card bills are charged on the 1st and retried;
-- invoice bills are sent by hand and marked paid.
CREATE TABLE billing_bills (
    id                    BIGSERIAL PRIMARY KEY,
    org_id                BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    period                TEXT NOT NULL CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'),
    method                TEXT NOT NULL CHECK (method IN ('card', 'invoice')),
    amount_cents          BIGINT NOT NULL CHECK (amount_cents > 0),
    currency              TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'paid', 'failed', 'void')),
    attempts              INTEGER NOT NULL DEFAULT 0,
    last_error            TEXT NOT NULL DEFAULT '',
    next_attempt_at       TIMESTAMPTZ,
    stripe_payment_intent TEXT NOT NULL DEFAULT '',
    invoice_number        TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at               TIMESTAMPTZ,
    UNIQUE (org_id, period)
);
CREATE INDEX idx_billing_bills_due ON billing_bills(next_attempt_at) WHERE state = 'open' AND method = 'card';

-- A top-up an organisation buys by card: pending until paid.
CREATE TABLE billing_top_ups (
    id                  BIGSERIAL PRIMARY KEY,
    org_id              BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    buyer_user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    amount_cents        BIGINT NOT NULL CHECK (amount_cents > 0),
    bonus_cents         BIGINT NOT NULL DEFAULT 0 CHECK (bonus_cents >= 0),
    currency            TEXT NOT NULL,
    provider            TEXT NOT NULL,
    provider_session_id TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'paid', 'failed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at             TIMESTAMPTZ
);
CREATE INDEX idx_billing_top_ups_org ON billing_top_ups(org_id, created_at DESC);

-- The balance's entries, in öre: what came in is positive, what teams
-- and phones cost negative. A team is charged once a game (round, for a
-- hosted event's games; 0 otherwise), a phone once.
CREATE TABLE billing_entries (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('top_up', 'bonus', 'payment', 'team', 'phone', 'refund', 'adjustment')),
    amount_cents BIGINT NOT NULL,
    currency     TEXT NOT NULL,
    event_id     BIGINT REFERENCES events(id) ON DELETE SET NULL,
    team_id      BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    slot_id      BIGINT REFERENCES player_slots(id) ON DELETE SET NULL,
    round        INTEGER NOT NULL DEFAULT 0,
    top_up_id    BIGINT REFERENCES billing_top_ups(id) ON DELETE SET NULL,
    bill_id      BIGINT REFERENCES billing_bills(id) ON DELETE SET NULL,
    note         TEXT NOT NULL DEFAULT '',
    created_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_billing_entries_org ON billing_entries(org_id, created_at);
CREATE UNIQUE INDEX idx_billing_entries_team ON billing_entries(team_id, round) WHERE kind = 'team';
CREATE UNIQUE INDEX idx_billing_entries_phone ON billing_entries(slot_id) WHERE kind = 'phone';
CREATE UNIQUE INDEX idx_billing_entries_top_up ON billing_entries(top_up_id, kind) WHERE top_up_id IS NOT NULL;
CREATE UNIQUE INDEX idx_billing_entries_bill ON billing_entries(bill_id) WHERE kind = 'payment';

-- Hosted games: a number of teams a game (team_limit) for a number of
-- games (game_limit), 0 for no limit, in place of blocks of 15 teams.
-- An organisation's own hosted event has neither: each team is charged.
-- A family's party holds the teams it paid for, for a few games.
ALTER TABLE events
    ADD COLUMN team_limit SMALLINT NOT NULL DEFAULT 0 CHECK (team_limit >= 0),
    ADD COLUMN game_limit SMALLINT NOT NULL DEFAULT 0 CHECK (game_limit >= 0);
UPDATE events SET team_limit = CASE WHEN hosted_blocks = 0 THEN 3 ELSE hosted_blocks * 15 END
WHERE party AND billing = 'prepaid';
ALTER TABLE events DROP COLUMN hosted_blocks;

-- A hosted purchase counts teams: a party's teams, or teams added to a
-- game ('hosted_teams', once 'hosted_blocks').
ALTER TABLE ticket_purchases
    DROP CONSTRAINT ticket_purchases_kind_check,
    DROP CONSTRAINT ticket_purchases_team_size_check;
UPDATE ticket_purchases SET kind = 'hosted_teams' WHERE kind = 'hosted_blocks';
ALTER TABLE ticket_purchases RENAME COLUMN blocks TO teams;
UPDATE ticket_purchases SET teams = teams * 15 WHERE kind IN ('hosted', 'hosted_teams', 'party');
ALTER TABLE ticket_purchases
    ADD CONSTRAINT ticket_purchases_kind_check CHECK (kind IN ('ticket', 'party', 'hosted', 'hosted_teams')),
    ADD CONSTRAINT ticket_purchases_team_size_check CHECK (
        team_size BETWEEN 1 AND 50 OR (kind IN ('hosted', 'hosted_teams') AND team_size = 0));
