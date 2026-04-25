-- Self-serve signup adds an email-verification step. Existing users
-- (seeded admins, in-org users created by an org_admin) had no email
-- column; they're grandfathered as already-verified so the new gates
-- don't lock them out of features they had yesterday. Only fresh
-- signups start at email_verified = 0.

ALTER TABLE users ADD COLUMN email CITEXT;
ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0;

-- Partial unique index: NULL email is allowed for grandfathered rows
-- (and for any future admin-seeded user without an email), but two
-- non-NULL email rows can't coexist case-insensitively (CITEXT).
CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;

-- Mark every pre-migration row verified so role-gated actions keep
-- working without manual cleanup.
UPDATE users SET email_verified = 1 WHERE email IS NULL;

-- One-shot tokens for "verify your email" links. purpose leaves room
-- for the upcoming forgot-password flow to reuse this table without
-- another migration; verify-purpose tokens have a 1h TTL set at
-- creation time.
CREATE TABLE email_verifications (
    token       TEXT PRIMARY KEY,
    user_id     BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose     TEXT    NOT NULL DEFAULT 'verify',
    created_at  TEXT    NOT NULL DEFAULT now_text(),
    expires_at  TEXT    NOT NULL
);

CREATE INDEX idx_email_verifications_user ON email_verifications(user_id, expires_at);
