-- Passkey signing for human listing reviewers (reviewer_passkeys.go).
-- A reviewer registers a passkey; every decision they make in the
-- console is signed with it, over a challenge that is a hash of the
-- decision itself: the action, the listing, the revision reviewed and
-- the reason. The private key never leaves the reviewer's device, so
-- the server can't make a signed decision on its own. Decisions an AI
-- assistant makes through an API token are not signed, and say so.

CREATE TABLE reviewer_passkeys (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    -- The webauthn.Credential as registered: public key, sign count.
    credential    JSONB NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_reviewer_passkeys_user ON reviewer_passkeys (user_id) WHERE revoked_at IS NULL;

-- A passkey ceremony in progress: registering one, or signing one
-- decision. Used once, then deleted; stale ones are ignored.
CREATE TABLE reviewer_passkey_sessions (
    id         TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('register', 'sign')),
    session    JSONB NOT NULL,
    -- For signing: the decision the challenge was made from.
    decision   JSONB,
    expires_at TIMESTAMPTZ NOT NULL
);

-- A signed decision carries what checks it, inside the hash chain: the
-- passkey and a copy of its public key (so it stays checkable when the
-- passkey or the account is gone), the nonce the challenge was made
-- with, and what the authenticator returned.
ALTER TABLE listing_review_log
    ADD COLUMN sig_credential_id      TEXT NOT NULL DEFAULT '',
    ADD COLUMN sig_public_key         TEXT NOT NULL DEFAULT '',
    ADD COLUMN sig_nonce              TEXT NOT NULL DEFAULT '',
    ADD COLUMN sig_authenticator_data TEXT NOT NULL DEFAULT '',
    ADD COLUMN sig_client_data        TEXT NOT NULL DEFAULT '',
    ADD COLUMN sig_signature          TEXT NOT NULL DEFAULT '';
