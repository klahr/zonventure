-- Stripe Connect support for the /store marketplace flow.
--
-- Each org that wants to sell quest tickets on the platform onboards
-- a Standard Connect account; ticket purchases destination-charge
-- against that account with the platform's application fee taken
-- off the top. The columns below cache the account id and the two
-- status flags Stripe surfaces (`charges_enabled`, `payouts_enabled`)
-- so the listing query and the quest-create gate don't have to round-
-- trip to Stripe on every page load. The cache is refreshed by the
-- `account.updated` webhook the platform endpoint subscribes to.
--
-- All columns are nullable / default-zero so the existing rows in a
-- live database survive the migration unchanged. An org with no
-- Connect account simply can't sell on /store; the backend gates
-- visibility on charges_enabled when the platform fee is configured.
--
-- Self-host deployments leave the platform fee at 0 and never
-- exercise this code path; the columns sit empty without harm.
ALTER TABLE organizations
    ADD COLUMN stripe_connect_account_id      TEXT,
    -- charges_enabled is the on-the-wire flag Stripe sets once the
    -- account has cleared identity verification AND can accept
    -- charges. It's the load-bearing predicate for "is this org
    -- ready to sell?" — payouts_enabled tracks separately because
    -- charges can be on while payouts are paused (e.g. missing
    -- bank info).
    ADD COLUMN stripe_connect_charges_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN stripe_connect_payouts_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- Best-effort timestamp of the last sync from Stripe (manual
    -- refresh button or account.updated webhook). Used by the
    -- admin UI to surface "verified 3 days ago" so the operator
    -- knows whether the cached status is fresh.
    ADD COLUMN stripe_connect_synced_at       TEXT;

-- One account per org. Multiple orgs sharing one Connect account
-- would route money cross-tenant, which is exactly the bug Connect
-- exists to prevent.
CREATE UNIQUE INDEX idx_organizations_stripe_connect_account
    ON organizations(stripe_connect_account_id)
    WHERE stripe_connect_account_id IS NOT NULL;

-- Per-purchase platform-fee record. Captured at checkout creation
-- time (not at webhook receipt) so a fee-formula change later
-- doesn't retroactively alter what was charged on already-completed
-- sales. Default 0 covers (a) historical rows from before this
-- migration and (b) self-host deployments where ZV_PLATFORM_FEE_*
-- is unset.
ALTER TABLE ticket_purchases
    ADD COLUMN application_fee_cents BIGINT NOT NULL DEFAULT 0;
