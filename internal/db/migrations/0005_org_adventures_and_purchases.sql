-- Opens up authoring beyond global admins: adventures and library
-- puzzles can now be owned by an organization, and org users can
-- purchase team-slot assignments through a (simulated, later Stripe)
-- checkout flow.
--
-- Adventures with org_id = NULL are the global admin-owned catalog,
-- visible to everyone; non-NULL adventures are private to their org.
-- Same story for library puzzles (orphan adventure_pages with
-- adventure_id = NULL): org_id scopes who can see / edit the stub.
ALTER TABLE adventures ADD COLUMN org_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE adventures ADD COLUMN created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX idx_adventures_org ON adventures(org_id);

-- Library orphan puzzles get their own org scope; attached puzzles
-- inherit via their adventure. org_id is NULL for admin-owned library
-- rows to match the adventure convention.
ALTER TABLE adventure_pages ADD COLUMN org_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX idx_adventure_pages_org ON adventure_pages(org_id);

-- Purchases are the revenue mechanism: an arranger buys N team slots
-- for an adventure, and on paid we create a normal adventure_assignment
-- row (state remains unchanged downstream — the existing run-creation
-- flow just works).
--
-- provider lets the front-end pick between the simulated confirm-button
-- flow used in dev and a real Stripe Checkout Session later. The
-- provider_session_id column stores Stripe's cs_test_... id so webhooks
-- can correlate.
CREATE TABLE purchases (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id              INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    buyer_user_id       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    adventure_slug      TEXT NOT NULL,
    max_teams           INTEGER NOT NULL CHECK (max_teams > 0),
    amount_cents        INTEGER NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'USD',
    provider            TEXT NOT NULL DEFAULT 'simulated',
    provider_session_id TEXT,
    state               TEXT NOT NULL CHECK (state IN ('pending','paid','failed','refunded')) DEFAULT 'pending',
    assignment_id       INTEGER REFERENCES adventure_assignments(id) ON DELETE SET NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    paid_at             TEXT
);

CREATE INDEX idx_purchases_org   ON purchases(org_id, created_at);
CREATE INDEX idx_purchases_state ON purchases(state);
