-- One phone per player: the first time a slot session is used by the
-- player app it becomes active and every other session of that slot is
-- marked replaced, which signs those phones out with "opened on another
-- phone". Sessions that exist already count as active, so the upgrade
-- signs no one out.
ALTER TABLE slot_sessions ADD COLUMN active BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE slot_sessions ADD COLUMN replaced BOOLEAN NOT NULL DEFAULT false;
UPDATE slot_sessions SET active = true;
