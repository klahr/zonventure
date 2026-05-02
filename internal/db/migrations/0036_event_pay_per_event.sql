-- Move billing from purchase-time (license-to-run) to event-creation
-- (metered). Two structural changes here:
--
-- 1. adventures.forked_from_slug — lineage tracking. NULL = author
--    wrote it from scratch in their own org (events on it stay
--    free); non-NULL = the adventure originated as a fork of a
--    Catalog row (events on it are billable). The slug is the
--    source's slug at the time of the fork; if the source is later
--    renamed or deleted, this column doesn't auto-update — we keep
--    a frozen reference so a buyer's history isn't rewritten by
--    upstream changes.
--
-- 2. events.state gains a 'pending' value used while a Stripe
--    Checkout session is in flight. The event row is created up
--    front (so we have a stable id to wire the Stripe session to)
--    but it isn't visible to anyone except the buyer until the
--    webhook flips it to 'scheduled'. The retention sweeper drops
--    pendings older than 30 minutes — covers Stripe-cancelled,
--    closed-tab, network-failed sessions.
--
-- 3. reuse_mode column is dropped. Under the metered model every
--    Catalog adventure is forkable — the as_is/template distinction
--    no longer earns its keep. Existing data is irrelevant; the
--    Catalog detail page paints one "Use this adventure" CTA for
--    everyone.

ALTER TABLE adventures ADD COLUMN forked_from_slug TEXT;

-- Drop the now-unused reuse_mode column. (The CHECK constraint
-- attached to it goes with the column.)
ALTER TABLE adventures DROP COLUMN reuse_mode;

-- Postgres doesn't support ALTER COLUMN ... DROP CHECK directly on
-- a CHECK that was inlined into the column; rebuild the constraint
-- to admit 'pending' alongside the existing states.
ALTER TABLE events DROP CONSTRAINT IF EXISTS adventure_runs_state_check;
ALTER TABLE events DROP CONSTRAINT IF EXISTS events_state_check;
ALTER TABLE events ADD CONSTRAINT events_state_check
  CHECK (state IN ('pending','scheduled','running','paused','completed','cancelled'));
