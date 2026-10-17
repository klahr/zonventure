-- An open-mode Google sign-in bridges to the TOTP step too.
ALTER TABLE auth_totp_challenges DROP CONSTRAINT auth_totp_challenges_origin_check;
ALTER TABLE auth_totp_challenges ADD CONSTRAINT auth_totp_challenges_origin_check
    CHECK (origin IN ('login','reset','oidc'));
