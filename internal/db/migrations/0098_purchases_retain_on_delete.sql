-- Payment records must survive org / event deletion: Swedish
-- bokföringslagen requires keeping them for seven years. Both FKs
-- used to CASCADE, so deleting an org (directly, or via the retention
-- sweep collapsing an org whose last member was deleted) wiped every
-- purchase and ticket purchase it had. Detach them instead.
ALTER TABLE purchases ALTER COLUMN org_id DROP NOT NULL;
ALTER TABLE purchases DROP CONSTRAINT purchases_org_id_fkey;
ALTER TABLE purchases
    ADD CONSTRAINT purchases_org_id_fkey
    FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE SET NULL;

ALTER TABLE ticket_purchases ALTER COLUMN event_id DROP NOT NULL;
ALTER TABLE ticket_purchases DROP CONSTRAINT ticket_purchases_event_id_fkey;
ALTER TABLE ticket_purchases
    ADD CONSTRAINT ticket_purchases_event_id_fkey
    FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE SET NULL;

-- Detached rows drop out of the session dedupe index so an org bundle
-- re-imported after the original was deleted can recreate them.
DROP INDEX idx_ticket_purchases_session;
CREATE UNIQUE INDEX idx_ticket_purchases_session
    ON ticket_purchases(stripe_session_id)
    WHERE stripe_session_id IS NOT NULL AND event_id IS NOT NULL;

-- The retained ticket row keeps amounts, currency, state and Stripe
-- ids; the buyer's email is personal data that goes with the event.
-- A trigger rather than handler code so every deletion path (admin
-- org delete, retention sweep, failed-import cleanup) is covered.
CREATE OR REPLACE FUNCTION zv_ticket_purchases_anonymise() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    NEW.buyer_email := '';
    RETURN NEW;
END;
$$;

CREATE TRIGGER ticket_purchases_anonymise
    BEFORE UPDATE OF event_id ON ticket_purchases
    FOR EACH ROW
    WHEN (OLD.event_id IS NOT NULL AND NEW.event_id IS NULL)
    EXECUTE FUNCTION zv_ticket_purchases_anonymise();
