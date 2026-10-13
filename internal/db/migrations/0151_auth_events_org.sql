-- An auth event about an organisation rather than one of its users
-- (its two-factor requirement turned on or off): org_id shows it to
-- the organisation's admins in Settings → Audit, whoever acted.
ALTER TABLE auth_events ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
