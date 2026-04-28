-- Stripe customer ID per org. Captured from the
-- checkout.session.completed webhook on the first completed purchase
-- and reused for billing-portal sessions ("Update payment method"
-- and the Stripe-hosted invoice list). Nullable: orgs that haven't
-- completed a real Stripe checkout (self-host without keys, or
-- before their first purchase) have no customer yet, and the
-- Customer Portal handler refuses 409 in that case.
ALTER TABLE organizations ADD COLUMN stripe_customer_id TEXT;
