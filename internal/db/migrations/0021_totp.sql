-- TOTP 2FA for arranger accounts.
--
-- The secret is stored encrypted at rest with AES-GCM, key from the
-- ZV_TOTP_KEY env var. A 12-byte nonce is prepended to the ciphertext
-- so the BYTEA column is self-describing for the unwrap routine.
--
-- totp_enabled gates the login flow; an unfinished enrolment leaves a
-- pending secret on the row with totp_enabled=false so the user can
-- abandon enrolment without locking the account.

ALTER TABLE users ADD COLUMN totp_secret_encrypted BYTEA;
ALTER TABLE users ADD COLUMN totp_enabled         BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN totp_enrolled_at     TEXT;

-- Single-use recovery codes the user prints/saves at enrolment. Hashed
-- with bcrypt (same idiom as password_hash) so a DB leak doesn't reveal
-- them. used_at IS NOT NULL means consumed.
CREATE TABLE user_recovery_codes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT   NOT NULL,
    used_at    TEXT,
    created_at TEXT   NOT NULL DEFAULT now_text()
);

CREATE INDEX user_recovery_codes_user_id_idx ON user_recovery_codes(user_id);

-- Short-lived rows that bridge a successful password (or password-reset)
-- step with the follow-up TOTP verify. The retention sweeper deletes
-- expired rows; ConsumeChallenge deletes the row inline on success.
--
-- origin distinguishes the two surfaces so a "reset" challenge can't
-- be redeemed for a normal login session before the password change
-- has taken effect on the user row, and vice versa.
CREATE TABLE auth_totp_challenges (
    token       TEXT   PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    origin      TEXT   NOT NULL CHECK (origin IN ('login','reset')),
    expires_at  TEXT   NOT NULL,
    ip          TEXT,
    user_agent  TEXT,
    created_at  TEXT   NOT NULL DEFAULT now_text()
);

CREATE INDEX auth_totp_challenges_expires_at_idx ON auth_totp_challenges(expires_at);
