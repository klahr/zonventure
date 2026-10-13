-- A party's host link, kept so that "Email me the host page" can send
-- the link the host already has instead of a new one that would stop the
-- open host page working. Only an explicit "Replace link" on the host
-- page rotates it. host_token_hash stays the lookup. The link is sealed
-- with ZV_TOTP_KEY, so a database dump alone does not reveal it; without
-- that key it is not kept and mailing the link again replaces it. NULL
-- for parties made before this, until the host opens their page.
ALTER TABLE events ADD COLUMN host_token_sealed BYTEA;
