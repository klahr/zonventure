-- Hosted event pricing: the platform charges a pass per started block
-- of 15 teams a game may hold. Two new purchase kinds ride the ticket
-- flow: 'hosted', an arranger hosting their own adventure ("Host it"),
-- whose event is made when the payment lands, in buyer_org_id from
-- adventure_slug; and 'hosted_blocks', more teams for an existing
-- hosted event (event_id). blocks is how many blocks were bought; a
-- hosted purchase buys no players, so its team_size is 0.
ALTER TABLE ticket_purchases DROP CONSTRAINT IF EXISTS ticket_purchases_kind_check;
ALTER TABLE ticket_purchases DROP CONSTRAINT IF EXISTS ticket_purchases_team_size_check;
ALTER TABLE ticket_purchases
    ADD CONSTRAINT ticket_purchases_kind_check CHECK (kind IN ('ticket', 'party', 'hosted', 'hosted_blocks')),
    ADD CONSTRAINT ticket_purchases_team_size_check CHECK (
        team_size BETWEEN 1 AND 50 OR (kind IN ('hosted', 'hosted_blocks') AND team_size = 0)),
    ADD COLUMN blocks          SMALLINT NOT NULL DEFAULT 0 CHECK (blocks >= 0),
    ADD COLUMN buyer_org_id    BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    ADD COLUMN adventure_slug  TEXT NOT NULL DEFAULT '';
