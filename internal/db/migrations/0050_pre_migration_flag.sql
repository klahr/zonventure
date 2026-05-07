-- Pre-migration flag on purchase rows.
--
-- When an org-bundle is imported from another zv instance, the
-- imported `purchases` and `ticket_purchases` rows carry the
-- ORIGINAL deployment's `stripe_charge_id`. Those charge IDs live
-- on the source platform's Stripe account, not on this instance's
-- account. Calling Refund against them via this instance's Stripe
-- credentials would either 404 (the charge isn't on this account)
-- or — worse — match an unrelated charge if the IDs happen to
-- collide.
--
-- The fix is a per-row flag the importer sets to TRUE so the
-- refund handler can refuse with a clear "refund this through the
-- pre-migration Stripe dashboard" message rather than blindly
-- handing the stale charge ID to the provider. New rows on this
-- instance default to FALSE; existing rows backfill to FALSE
-- (they pre-date any migration the row could have come from).
--
-- Why a column rather than inferring from the absence of the
-- charge ID on this account: a charge created on this account but
-- subsequently deleted from Stripe still has the ID on file in our
-- DB; we don't want to misclassify those as legacy. Explicit flag
-- set at import time is the only reliable signal.

ALTER TABLE purchases
    ADD COLUMN pre_migration BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE ticket_purchases
    ADD COLUMN pre_migration BOOLEAN NOT NULL DEFAULT FALSE;
