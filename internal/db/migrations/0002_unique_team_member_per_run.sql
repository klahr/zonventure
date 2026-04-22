-- A player must not be on more than one team in the same run. The handler
-- also checks this so it can produce a friendly error; the trigger is the
-- last-line-of-defence so no direct SQL can break the invariant.
CREATE TRIGGER prevent_duplicate_team_members
BEFORE INSERT ON team_members
FOR EACH ROW
BEGIN
  SELECT RAISE(ABORT, 'user is already on a team in this run')
  WHERE EXISTS (
    SELECT 1
    FROM team_members tm2
    JOIN teams t2 ON t2.id = tm2.team_id
    JOIN teams tnew ON tnew.id = NEW.team_id
    WHERE tm2.user_id = NEW.user_id
      AND t2.run_id = tnew.run_id
      AND tm2.team_id != NEW.team_id
  );
END;
