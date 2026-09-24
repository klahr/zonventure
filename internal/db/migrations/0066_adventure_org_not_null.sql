-- Every adventure and library puzzle has an owning org now: 0065 moved
-- the ownerless ones into the publisher org, and nothing can create
-- ownerless ones any more (the platform admin doesn't author). Pin
-- that down. Puzzles attached to an adventure keep org_id NULL — they
-- ride their adventure's owner.

ALTER TABLE adventures ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE adventure_pages ADD CONSTRAINT adventure_pages_have_owner
    CHECK (adventure_id IS NOT NULL OR org_id IS NOT NULL);
