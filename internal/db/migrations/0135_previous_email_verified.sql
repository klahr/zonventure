-- Whether the previous email was verified, so reverting an email change
-- restores that state instead of carrying over the new address's
-- verification.
ALTER TABLE users ADD COLUMN previous_email_verified INTEGER;
