-- Trusted orgs can buy player slots on invoice instead of paying via
-- Stripe Checkout. Set/cleared by global admins; the buy modal shows
-- the "Add to invoice" option only when this is on. Off-platform
-- invoice generation; the platform just tags the resulting purchase
-- with provider='invoice' so the operator knows what to bill.

ALTER TABLE organizations ADD COLUMN trusted INTEGER NOT NULL DEFAULT 0;
