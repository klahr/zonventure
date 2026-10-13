-- A global admin with the fixed Support role: reads the admin console,
-- may only verify emails, resend host links and reset two-factor sign-in.
ALTER TABLE users ADD COLUMN is_support BOOLEAN NOT NULL DEFAULT FALSE;
