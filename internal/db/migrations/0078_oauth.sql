-- OAuth 2.1 for the MCP endpoint, so a client such as a claude.ai
-- connector can sign a user in instead of being handed a pasted token.
--
-- A client registers itself (RFC 7591) and gets a client_id; it has no
-- secret — every client is public and proves itself with PKCE instead.
CREATE TABLE oauth_clients (
    id            BIGSERIAL PRIMARY KEY,
    client_id     TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    redirect_uris JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- An authorization code: single use, short-lived, bound to the client,
-- redirect URI and PKCE challenge it was issued for.
CREATE TABLE oauth_codes (
    code_hash      BYTEA PRIMARY KEY,
    client_id      BIGINT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    scopes         JSONB NOT NULL,
    org_id         BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    expires_at     TIMESTAMPTZ NOT NULL
);

-- An approved client is an mcp_tokens row: the same scopes, lookup and
-- revocation as a pasted token, listed in Settings → API tokens. Its
-- access token expires and is replaced, with its refresh token, on each
-- refresh. The refresh token it replaced is kept to spot reuse — someone
-- replaying a stolen one — which revokes the row.
ALTER TABLE mcp_tokens
    ADD COLUMN expires_at             TIMESTAMPTZ,
    ADD COLUMN oauth_client_id        BIGINT REFERENCES oauth_clients(id) ON DELETE CASCADE,
    ADD COLUMN refresh_hash           BYTEA UNIQUE,
    ADD COLUMN previous_refresh_hash  BYTEA,
    ADD COLUMN refresh_expires_at     TIMESTAMPTZ;

-- The organisation a token acts in, for someone in several; NULL is
-- their first (lowest id), as before. A token pinned to an organisation
-- the user has since left stops working rather than moving elsewhere.
ALTER TABLE mcp_tokens
    ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
