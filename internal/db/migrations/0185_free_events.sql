-- billing 'free': a self-guided event whose tickets cost nothing, to the
-- player or to the organisation — a free trial in the store. Its price
-- is 0, and the store hands out a ticket for an email address instead
-- of opening a checkout. Only the site's own organisation (ZV_HOME_ORG)
-- sets it: anyone else's free tickets would be the platform's cost.
ALTER TABLE events DROP CONSTRAINT events_billing_check;
ALTER TABLE events ADD CONSTRAINT events_billing_check CHECK (billing IN ('teams', 'prepaid', 'free'));
