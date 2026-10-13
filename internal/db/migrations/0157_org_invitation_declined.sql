-- A declined invitation stays, marked, so the organisation's admins see
-- "Declined" among their pending invitations. Inviting the person again
-- clears it; withdrawing it deletes the row as before.
ALTER TABLE org_invitations ADD COLUMN declined_at TIMESTAMPTZ;
