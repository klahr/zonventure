-- Two-factor enforcement: an organisation may require it of every
-- member, and a session records whether SSO minted it (SSO satisfies
-- the requirement). The instance-wide admin switch lives in settings.
ALTER TABLE organizations ADD COLUMN require_totp BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE sessions ADD COLUMN via_sso BOOLEAN NOT NULL DEFAULT FALSE;
