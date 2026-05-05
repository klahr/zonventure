-- Per-user API tokens for the Model Context Protocol endpoint
-- (POST /mcp). MCP clients — Claude desktop, claude.ai connectors —
-- send a token in the Authorization header and the server resolves
-- it back to the user that minted it; the resulting request runs
-- with the same authoring policy as if the user were logged into
-- /arrange.
--
-- Tokens are stored as a sha256 of the plaintext, not the plaintext
-- itself: a DB compromise leaks the ability to mint adventures for
-- the affected users (which the existing session table already
-- offers via session theft) but does NOT leak credentials usable
-- against any external system the user might also paste the token
-- into. token_prefix keeps the first ~8 visible characters so the
-- settings UI can show "zvmcp_AB…" alongside the user's chosen
-- name without re-revealing the secret.

CREATE TABLE mcp_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BYTEA   NOT NULL UNIQUE,
    -- Human label the user picked at creation time. Free text,
    -- length-capped in the handler.
    name         TEXT    NOT NULL,
    -- First ~8 characters of the plaintext token (including the
    -- "zvmcp_" prefix) so the list view can disambiguate without
    -- having access to the full secret.
    token_prefix TEXT    NOT NULL,
    created_at   TEXT    NOT NULL DEFAULT now_text(),
    -- Updated best-effort on every successful MCP call. NULL means
    -- "never used since creation". Used by the settings UI to
    -- surface stale tokens the user can prune.
    last_used_at TEXT
);

CREATE INDEX idx_mcp_tokens_user ON mcp_tokens(user_id);
