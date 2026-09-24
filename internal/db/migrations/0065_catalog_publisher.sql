-- The platform catalog moves from "adventures with no org" to "the
-- adventures of the publisher org". The platform admin stops authoring:
-- a regular organisation — at most one, flagged is_publisher — writes
-- the catalog, and the admin only curates it (lists it, commissions it
-- to other orgs).
--
-- Existing admin-owned content (adventures.org_id NULL, and library
-- puzzles with neither an adventure nor an org) moves into a publisher
-- org created here. A database with nothing to adopt gets no org; the
-- operator flags one from the Organizations page instead.
--
-- Shares, assignments, events and purchases name adventures by slug,
-- so they keep pointing at the same rows.

ALTER TABLE organizations ADD COLUMN is_publisher BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX organizations_single_publisher ON organizations ((TRUE)) WHERE is_publisher;

DO $$
DECLARE
    pid BIGINT;
    nm TEXT := 'Zonventure Catalog';
    n INTEGER := 2;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM adventures WHERE org_id IS NULL)
       AND NOT EXISTS (SELECT 1 FROM adventure_pages WHERE adventure_id IS NULL AND org_id IS NULL) THEN
        RETURN;
    END IF;
    SELECT id INTO pid FROM organizations WHERE is_publisher;
    IF pid IS NULL THEN
        WHILE EXISTS (SELECT 1 FROM organizations WHERE name = nm) LOOP
            nm := 'Zonventure Catalog ' || n::TEXT;
            n := n + 1;
        END LOOP;
        INSERT INTO organizations(name, is_publisher) VALUES (nm, TRUE) RETURNING id INTO pid;
    END IF;
    UPDATE adventures SET org_id = pid WHERE org_id IS NULL;
    UPDATE adventure_pages SET org_id = pid WHERE adventure_id IS NULL AND org_id IS NULL;
END;
$$;
