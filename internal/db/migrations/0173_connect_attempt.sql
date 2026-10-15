-- A platform admin can disconnect an organisation's Stripe account (one
-- onboarded with the wrong company, say). Each disconnect bumps the
-- attempt, which is in the request id the next account is created
-- with: Stripe answers a reused id with the old account for 30 days.
ALTER TABLE organizations ADD COLUMN stripe_connect_attempt INT NOT NULL DEFAULT 0;
