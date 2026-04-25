-- Email-change flow needs to remember the pending new address until
-- the user clicks the link. Adding a generic data column to
-- email_verifications keeps the table flexible for any future
-- purpose that wants per-token state, without spinning up a side
-- table. Existing verify and forgot-password rows leave it NULL.

ALTER TABLE email_verifications ADD COLUMN data TEXT;
