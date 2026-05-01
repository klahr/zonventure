-- OpenID Connect identity binding for arranger accounts.
--
-- When SSO is enabled (ZV_OIDC_ENABLED=1), arrangers from the
-- configured Google Workspace domain sign in via the IdP rather than
-- a password. The IdP's stable user identifier (the `sub` claim) is
-- recorded here alongside the issuer so subsequent logins look up
-- the account by (oidc_provider, oidc_subject) — the email field
-- alone is too weak for primary identity (admins can change a user's
-- primary email in Workspace, and aliases collide).
--
-- Both columns are nullable: password-only accounts (the default for
-- self-hosters who don't enable SSO) leave them NULL. The unique
-- index is partial so multiple password-only rows can coexist.
ALTER TABLE users ADD COLUMN oidc_provider TEXT;
ALTER TABLE users ADD COLUMN oidc_subject  TEXT;

CREATE UNIQUE INDEX idx_users_oidc_provider_subject
  ON users (oidc_provider, oidc_subject)
  WHERE oidc_provider IS NOT NULL;
