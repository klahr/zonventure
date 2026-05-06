-- Per-token scope set so a user can mint a narrower MCP credential
-- than their full account powers. Scopes can only NARROW (a token
-- never grants authority beyond the user's role); think of them as a
-- second filter applied after the existing org-policy checks.
--
-- Two scopes are recognised today:
--   read   — list/get/validate tools (no DB writes, no email)
--   write  — every other tool (mutations, transitions, side effects)
--
-- JSONB (not TEXT[]) so the database/sql + pgx stdlib driver can
-- scan it back into []string via json.Unmarshal — same posture as
-- webhook_endpoints.event_kinds (see 0008_webhooks.sql).
--
-- Default ['read','write'] keeps every pre-existing token at full
-- powers so the migration is invisible to current users; the UI now
-- exposes the knob for new tokens.

ALTER TABLE mcp_tokens
    ADD COLUMN scopes JSONB NOT NULL DEFAULT '["read","write"]'::jsonb;
